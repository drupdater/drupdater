package cmd

import (
	"os"

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
	Use:   "drupdater repository-url token",
	Short: "Drupal Updater",
	Long:  `Drupal Updater is a tool to update Drupal dependencies and create merge requests.`,
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Silence default error handling
		cmd.SilenceUsage = true
		cmd.SilenceErrors = true

		// Parse command line arguments
		config.RepositoryURL = args[0]
		config.Token = args[1]

		// Initialize required services
		logger := NewLogger(config)
		cache := NewCache()

		// Create core service instances
		drush := drush.NewCLI(logger, cache)
		composer := composer.NewCLI(logger)
		drupalOrg := drupalorg.NewHTTPClient(logger)
		installer := drupal.NewInstaller(logger, drush, composer)
		vcsProviderFactory := codehosting.NewDefaultVcsProviderFactory()
		platform := vcsProviderFactory.Create(config.RepositoryURL, config.Token)
		git := repo.NewGitRepositoryService(logger)
		workflow := services.NewWorkflowBaseService(logger, config, drush, platform, git, installer, composer)

		// Register addons based on configuration
		addons := createAddons(logger, config, drush, composer, drupalOrg, git)

		// Register all addons as event subscribers
		for _, addon := range addons {
			event.AddSubscriber(addon)
		}

		// Start the update workflow
		err := workflow.StartUpdate(cmd.Context(), addons)
		if err != nil {
			logger.Sugar().Error(err)
			return err
		}
		logger.Info("update finished")
		return nil
	},
}

// createAddons creates and returns the list of addons to be used based on the configuration
func createAddons(
	logger *zap.Logger,
	config internal.Config,
	drush *drush.CLI,
	composer *composer.CLI,
	drupalOrg *drupalorg.HTTPClient,
	git *repo.GitRepositoryService,
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
		addon.NewComposerPatches1(logger, composer, drupalOrg),
		addon.NewComposerDiff(logger, composer),
		addon.NewUpdateHooks(logger, drush),
	)

	return addons
}

func init() {
	rootCmd.PersistentFlags().StringVar(&config.Branch, "branch", "main", "Branch")
	rootCmd.PersistentFlags().StringArrayVar(&config.Sites, "sites", []string{"default"}, "Sites")
	rootCmd.PersistentFlags().BoolVar(&config.Security, "security", false, "Only security updates. If true, only security updates will be applied.")
	rootCmd.PersistentFlags().BoolVar(&config.AutoMerge, "auto-merge", false, "Auto merge. If true, the merge request will be merged automatically.")
	rootCmd.PersistentFlags().BoolVar(&config.SkipCBF, "skip-cbf", false, "Skip CBF. If true, the PHPCBF will not be run.")
	rootCmd.PersistentFlags().BoolVar(&config.SkipRector, "skip-rector", false, "Skip Rector. If true, the Rector will not run to remove deprecated code.")
	rootCmd.PersistentFlags().BoolVar(&config.DryRun, "dry-run", false, "Dry run. If true, no branch and merge request will be created.")
	rootCmd.PersistentFlags().BoolVar(&config.Verbose, "verbose", false, "Verbose")
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
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
	log, _ := loggerConfig.Build()
	return log
}
