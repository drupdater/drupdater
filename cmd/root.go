package cmd

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/drupdater/drupdater/internal"
	"github.com/drupdater/drupdater/internal/addon"
	"github.com/drupdater/drupdater/internal/codehosting"
	"github.com/drupdater/drupdater/internal/services"
	"github.com/drupdater/drupdater/pkg/composer"
	"github.com/drupdater/drupdater/pkg/drupal"
	"github.com/drupdater/drupdater/pkg/drupalorg"
	"github.com/drupdater/drupdater/pkg/drush"
	"github.com/drupdater/drupdater/pkg/phpcs"
	"github.com/drupdater/drupdater/pkg/rector"
	"github.com/drupdater/drupdater/pkg/repo"
	"github.com/gookit/event"
	"github.com/maypok86/otter"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// config holds the application configuration
var config internal.Config

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "drupdater token",
	Short: "Drupal Updater",
	Long:  `Drupal Updater is a tool to update Drupal dependencies and create merge requests.`,
	Args:  cobra.ExactArgs(1),
	PreRunE: func(_ *cobra.Command, _ []string) error {
		// --clone needs an explicit repository URL; checkout mode derives it from origin.
		if config.Clone && config.RepositoryURL == "" {
			return errors.New("--repository-url is required with --clone")
		}
		// Validate the URL format when one is given.
		if config.RepositoryURL != "" {
			if _, err := url.ParseRequestURI(config.RepositoryURL); err != nil {
				return errors.New("invalid repository URL")
			}
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		// Silence default error handling
		cmd.SilenceUsage = true
		cmd.SilenceErrors = true

		// Parse command line arguments
		config.Token = args[0]

		// Initialize required services
		logger := NewLogger(config)
		cache := NewCache()

		// Create core service instances
		drush := drush.NewCLI(logger, cache)
		composer := composer.NewCLI(logger)
		drupalOrg := drupalorg.NewHTTPClient(logger)
		installer := drupal.NewInstaller(logger, drush, composer)
		git := repo.NewGitRepositoryService(logger)

		// In checkout mode the repository URL and target branch come from the checkout, so
		// they don't have to be passed in. --branch only applies to --clone.
		if !config.Clone {
			if config.RepositoryURL == "" {
				remoteURL, err := git.GetRemoteURL(config.WorkingDir)
				if err != nil {
					return fmt.Errorf("failed to determine repository URL from checkout (pass --repository-url or run inside a checkout): %w", err)
				}
				config.RepositoryURL = remoteURL
			}

			branch, err := resolveCheckoutBranch(git, config.WorkingDir)
			if err != nil {
				return err
			}
			config.Branch = branch
			logger.Info("using checkout", zap.String("url", config.RepositoryURL), zap.String("branch", config.Branch))
		}

		vcsProviderFactory := codehosting.NewDefaultVcsProviderFactory()
		platform := vcsProviderFactory.Create(config.RepositoryURL, config.Token)

		// Create the event dispatcher and register addons as subscribers
		addons := createAddons(logger, config, drush, composer, drupalOrg, git)
		dispatcher := createDispatcher(addons)

		workflow := services.NewWorkflowBaseService(logger, config, drush, platform, git, installer, composer, dispatcher)

		// Start the update workflow
		err := workflow.StartUpdate(cmd.Context(), addons)
		if err != nil {
			if err := handleWorkflowError(logger, err); err != nil {
				return err
			}
		} else {
			logger.Info("update finished")
		}
		return nil
	},
}

// resolveCheckoutBranch determines the MR target branch for checkout mode: the checkout's
// current branch, or — when it's in detached HEAD (the usual CI state) — the branch reported
// by the CI environment.
func resolveCheckoutBranch(git *repo.GitRepositoryService, workingDir string) (string, error) {
	branch, err := git.GetCurrentBranch(workingDir)
	if err != nil {
		return "", fmt.Errorf("failed to determine branch from checkout: %w", err)
	}
	if branch == "" {
		branch = cmp.Or(os.Getenv("GITHUB_REF_NAME"), os.Getenv("CI_COMMIT_REF_NAME"))
	}
	if branch == "" {
		return "", errors.New("could not determine the target branch: the checkout is in detached HEAD and no CI branch variable (GITHUB_REF_NAME, CI_COMMIT_REF_NAME) is set")
	}
	return branch, nil
}

// createAddons creates and returns the list of addons to be used based on the configuration
func createAddons(
	logger *zap.Logger,
	config internal.Config,
	drush addon.Drush,
	composer addon.Composer,
	drupalOrg addon.DrupalOrg,
	git addon.Repository,
) []internal.Addon {
	var addons []internal.Addon

	// Conditional addons
	if config.Security {
		addons = append(addons, addon.NewComposerAudit(logger, composer))
	}
	if !config.SkipCBF {
		phpcsRunner := phpcs.NewCLI(logger)
		addons = append(addons, addon.NewCodeBeautifier(logger, phpcsRunner, config, composer))
	}
	if !config.SkipRector {
		rectorRunner := rector.NewCLI(logger)
		addons = append(addons, addon.NewDeprecationsRemover(logger, rectorRunner, config, composer))
	}

	// Default addons
	addons = append(addons,
		addon.NewTranslationsUpdater(logger, drush, git),
		addon.NewComposerAllowPlugins(logger, composer),
		addon.NewComposerNormalizer(logger, composer),
		addon.NewComposerPatches1(logger, composer, drupalOrg, http.DefaultClient),
		addon.NewComposerDiff(logger, composer),
		addon.NewUpdateHooks(logger, drush),
	)

	return addons
}

// createDispatcher creates a new event manager and subscribes all addons to it.
func createDispatcher(addons []internal.Addon) services.EventDispatcher {
	dispatcher := event.NewManager("")
	for _, addon := range addons {
		dispatcher.AddSubscriber(addon)
	}
	return dispatcher
}

// handleWorkflowError logs AbortErrors as warnings (non-fatal) and all others as errors (fatal).
func handleWorkflowError(logger *zap.Logger, err error) error {
	if errors.As(err, &services.AbortError{}) {
		logger.Warn("update aborted", zap.Error(err))
		return nil
	}
	logger.Error("update failed", zap.Error(err))
	return err
}

func init() {
	rootCmd.PersistentFlags().StringVar(&config.Branch, "branch", "main", "Branch to update and target for the MR. Only used with --clone; in checkout mode it's taken from the checkout (or CI branch variable).")
	rootCmd.PersistentFlags().StringVar(&config.WorkingDir, "working-dir", ".", "Path to the existing checkout to update in place.")
	rootCmd.PersistentFlags().BoolVar(&config.Clone, "clone", false, "Clone the repository instead of using the existing checkout. Requires --repository-url. Intended for local testing.")
	rootCmd.PersistentFlags().StringVar(&config.RepositoryURL, "repository-url", "", "Repository URL. Required with --clone; otherwise derived from the checkout's origin remote.")
	rootCmd.PersistentFlags().StringArrayVar(&config.Sites, "sites", []string{"default"}, "Sites")
	rootCmd.PersistentFlags().BoolVar(&config.Security, "security", false, "Only security updates. If true, only security updates will be applied.")
	rootCmd.PersistentFlags().BoolVar(&config.SkipCBF, "skip-cbf", false, "Skip CBF. If true, the PHPCBF will not be run.")
	rootCmd.PersistentFlags().BoolVar(&config.SkipRector, "skip-rector", false, "Skip Rector. If true, the Rector will not run to remove deprecated code.")
	rootCmd.PersistentFlags().BoolVar(&config.DryRun, "dry-run", false, "Dry run. If true, no branch and merge request will be created.")
	rootCmd.PersistentFlags().BoolVar(&config.Verbose, "verbose", false, "Verbose")
	rootCmd.PersistentFlags().DurationVar(&config.Timeout, "timeout", 30*time.Minute, "Overall run timeout. Set to 0 to disable.")
}

func Execute() {
	// Cancel the workflow context on SIGINT/SIGTERM so cleanup runs on termination.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := rootCmd.ExecuteContext(ctx); err != nil {
		os.Exit(1)
	}
}

func NewCache() otter.Cache[string, string] {
	cache, _ := otter.MustBuilder[string, string](100).Build()
	return cache
}

func NewLogger(config internal.Config) *zap.Logger {
	loggerConfig := zap.NewDevelopmentConfig()
	loggerConfig.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder

	if !config.Verbose {
		loggerConfig.Level.SetLevel(zapcore.InfoLevel)
		loggerConfig.DisableCaller = true
		loggerConfig.DisableStacktrace = true
	}
	log, _ := loggerConfig.Build(zap.AddStacktrace(zapcore.ErrorLevel))
	return log
}
