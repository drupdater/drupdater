package cmd

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

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

// configFile is the path to .drupdater.yaml; empty means <working-dir>/.drupdater.yaml.
var configFile string

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "drupdater [token]",
	Short: "Drupal Updater",
	Long: `Drupal Updater is a tool to update Drupal dependencies and create merge requests.

The access token is read from the first argument, or from the DRUPDATER_TOKEN environment
variable when no argument is given (which keeps it out of the process list and shell history).

Project settings (sites, timeout, and which addons run) are read from .drupdater.yaml in the
working directory; override the path with --config. Run "drupdater addons" to list the addon
names you can set there. See the README for the full file format.`,
	Args: cobra.MaximumNArgs(1),
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

		// Initialize the logger first so config errors are reported (errors are silenced by Cobra).
		logger := NewLogger(config)

		// The token comes from the positional argument, or DRUPDATER_TOKEN when it's omitted.
		if len(args) == 1 {
			config.Token = args[0]
		} else {
			config.Token = os.Getenv("DRUPDATER_TOKEN")
		}
		if config.Token == "" {
			err := errors.New("no token provided: pass it as the argument or set DRUPDATER_TOKEN")
			logger.Error("missing token", zap.Error(err))
			return err
		}

		// Load per-project config from .drupdater.yaml (sites, timeout, addons). A missing file
		// falls back to built-in defaults.
		cfgPath := configFile
		if cfgPath == "" {
			cfgPath = filepath.Join(config.WorkingDir, ".drupdater.yaml")
		}
		found, err := internal.LoadConfigFile(cfgPath, &config)
		if err != nil {
			logger.Error("invalid configuration", zap.Error(err))
			return err
		}
		if err := validateAddons(config); err != nil {
			logger.Error("invalid configuration", zap.String("path", cfgPath), zap.Error(err))
			return err
		}
		logger.Debug("configuration loaded",
			zap.String("path", cfgPath),
			zap.Bool("file_found", found),
			zap.Strings("sites", config.Sites),
			zap.Duration("timeout", config.Timeout),
			zap.Strings("addons.normal", config.Addons.Normal),
			zap.Strings("addons.security", config.Addons.Security),
		)

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

			// CI mounts the checkout owned by a different user than the container runs as, so
			// the git binary (invoked by drush/composer) refuses it as "dubious ownership".
			// Mark it safe so those child processes can run git against it.
			ensureGitSafeDirectory(logger, config.WorkingDir)
		}

		vcsProviderFactory := codehosting.NewDefaultVcsProviderFactory()
		platform, err := vcsProviderFactory.Create(config.RepositoryURL, config.Token, logger)
		if err != nil {
			logger.Error("failed to create VCS provider", zap.Error(err))
			return err
		}

		// Create the event dispatcher and register addons as subscribers
		addons, err := createAddons(logger, config, drush, composer, drupalOrg, git)
		if err != nil {
			return err
		}
		dispatcher := createDispatcher(addons)

		workflow := services.NewWorkflowBaseService(logger, config, drush, platform, git, installer, composer, dispatcher)

		// Start the update workflow
		err = workflow.StartUpdate(cmd.Context(), addons)
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

// ensureGitSafeDirectory adds dir to git's global safe.directory list unless it (or "*") is
// already trusted, so repeated checkout-mode runs on a developer machine don't append a
// duplicate entry to the user's global gitconfig on every invocation.
func ensureGitSafeDirectory(logger *zap.Logger, dir string) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		logger.Warn("failed to resolve checkout path for safe.directory", zap.Error(err))
		return
	}

	if out, err := exec.Command("git", "config", "--global", "--get-all", "safe.directory").Output(); err == nil {
		for _, entry := range strings.Split(string(out), "\n") {
			if entry == abs || entry == "*" {
				return
			}
		}
	}

	if out, err := exec.Command("git", "config", "--global", "--add", "safe.directory", abs).CombinedOutput(); err != nil {
		logger.Warn("failed to mark checkout as a safe git directory", zap.String("output", string(out)), zap.Error(err))
	}
}

// addonDeps carries everything the addon constructors need.
type addonDeps struct {
	logger    *zap.Logger
	config    internal.Config
	drush     addon.Drush
	composer  addon.Composer
	drupalOrg addon.DrupalOrg
	git       addon.Repository
}

// addonRegistry maps the names used in .drupdater.yaml to their constructors.
var addonRegistry = map[string]func(addonDeps) internal.Addon{
	"composer_audit": func(d addonDeps) internal.Addon { return addon.NewComposerAudit(d.logger, d.composer) },
	"code_beautifier": func(d addonDeps) internal.Addon {
		return addon.NewCodeBeautifier(d.logger, phpcs.NewCLI(d.logger), d.config, d.composer)
	},
	"deprecations_remover": func(d addonDeps) internal.Addon {
		return addon.NewDeprecationsRemover(d.logger, rector.NewCLI(d.logger), d.config, d.composer)
	},
	"translations_updater":   func(d addonDeps) internal.Addon { return addon.NewTranslationsUpdater(d.logger, d.drush, d.git) },
	"composer_allow_plugins": func(d addonDeps) internal.Addon { return addon.NewComposerAllowPlugins(d.logger, d.composer) },
	"composer_normalizer":    func(d addonDeps) internal.Addon { return addon.NewComposerNormalizer(d.logger, d.composer) },
	"composer_patches": func(d addonDeps) internal.Addon {
		return addon.NewComposerPatches1(d.logger, d.composer, d.drupalOrg, http.DefaultClient)
	},
	"composer_diff": func(d addonDeps) internal.Addon { return addon.NewComposerDiff(d.logger, d.composer) },
	"update_hooks":  func(d addonDeps) internal.Addon { return addon.NewUpdateHooks(d.logger, d.drush) },
}

// mandatoryAddons always run, regardless of the .drupdater.yaml addon lists.
var mandatoryAddons = []string{"composer_allow_plugins", "composer_patches", "composer_diff", "update_hooks"}

// createAddons builds the addons to run: the mandatory ones plus the configurable ones listed
// for the active mode (security or regular) in the config. An unknown addon name is an error.
func createAddons(
	logger *zap.Logger,
	config internal.Config,
	drush addon.Drush,
	composer addon.Composer,
	drupalOrg addon.DrupalOrg,
	git addon.Repository,
) ([]internal.Addon, error) {
	deps := addonDeps{logger: logger, config: config, drush: drush, composer: composer, drupalOrg: drupalOrg, git: git}

	names := config.Addons.Normal
	if config.Security {
		names = config.Addons.Security
	}

	var addons []internal.Addon
	added := map[string]bool{}
	build := func(name string) error {
		if added[name] {
			return nil
		}
		factory, ok := addonRegistry[name]
		if !ok {
			return fmt.Errorf("unknown addon %q", name)
		}
		addons = append(addons, factory(deps))
		added[name] = true
		return nil
	}

	for _, name := range mandatoryAddons {
		if err := build(name); err != nil {
			return nil, err
		}
	}
	// composer_audit is mandatory in security mode.
	if config.Security {
		if err := build("composer_audit"); err != nil {
			return nil, err
		}
	}
	for _, name := range names {
		if err := build(name); err != nil {
			return nil, err
		}
	}

	return addons, nil
}

// validateAddons checks every addon named in either list is known, regardless of which mode
// will run, so a typo in addons.security is caught even on a normal run (and vice versa).
func validateAddons(config internal.Config) error {
	for _, name := range append(append([]string{}, config.Addons.Normal...), config.Addons.Security...) {
		if _, ok := addonRegistry[name]; !ok {
			return fmt.Errorf("unknown addon %q (run \"drupdater addons\" to list valid names)", name)
		}
	}
	return nil
}

// configurableAddons returns the sorted addon names a user can set in .drupdater.yaml — every
// registered addon except the ones that always run (mandatoryAddons and, in security mode,
// composer_audit).
func configurableAddons() []string {
	excluded := map[string]bool{"composer_audit": true}
	for _, n := range mandatoryAddons {
		excluded[n] = true
	}
	names := make([]string, 0, len(addonRegistry))
	for n := range addonRegistry {
		if !excluded[n] {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	return names
}

// addonsCmd lists the addon names that can be set in .drupdater.yaml.
var addonsCmd = &cobra.Command{
	Use:   "addons",
	Short: "List the addon names that can be set in .drupdater.yaml",
	Run: func(cmd *cobra.Command, _ []string) {
		out := cmd.OutOrStdout()
		fmt.Fprintln(out, "Addons you can set under addons.normal / addons.security in .drupdater.yaml:")
		for _, n := range configurableAddons() {
			fmt.Fprintf(out, "  %s\n", n)
		}
	},
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
	rootCmd.PersistentFlags().BoolVar(&config.Security, "security", false, "Only security updates. If true, only security updates will be applied.")
	rootCmd.PersistentFlags().BoolVar(&config.DryRun, "dry-run", false, "Dry run. If true, no branch and merge request will be created.")
	rootCmd.PersistentFlags().BoolVar(&config.Verbose, "verbose", false, "Verbose")
	rootCmd.PersistentFlags().StringVar(&configFile, "config", "", "Path to the config file (default: <working-dir>/.drupdater.yaml).")

	rootCmd.AddCommand(addonsCmd)
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
