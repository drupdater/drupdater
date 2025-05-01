package cmd

import (
	"context"
	"fmt"
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

var config internal.Config

var rootCmd = &cobra.Command{
	Use:   "drupdater <repository-url> <token>",
	Short: "Drupal Updater",
	Long:  `Drupal Updater is a tool to update Drupal dependencies and create merge requests.`,
	Args:  cobra.ExactArgs(2),
	Run: func(_ *cobra.Command, args []string) {
		config.RepositoryURL = args[0]
		config.Token = args[1]

		logger := NewLogger(config)
		cache := NewCache()
		drush := drush.NewCLI(logger, cache)
		composer := composer.NewCLI(logger)
		drupalOrg := drupalorg.NewHTTPClient(logger)
		settings := drupal.NewDefaultSettingsService(logger, drush, composer)
		installer := drupal.NewDefaultInstallerService(logger, drush, settings)
		git := repo.NewGitRepositoryService(logger)
		vcsProviderFactory := codehosting.NewDefaultVcsProviderFactory()
		updater := services.NewDefaultUpdater(logger, settings, git, config, composer, drupalOrg, drush)

		workflow := services.NewWorkflowBaseService(logger, config, updater, vcsProviderFactory, git, installer, composer)

		var strategy services.WorkflowStrategy
		strategy = services.NewDependencyUpdateStrategy(logger, config)
		if config.Security {
			strategy = services.NewSecurityUpdateStrategy(logger, config, composer)
		}
		ctx := context.Background()

		if !config.SkipCBF {
			phpcsRunner := phpcs.NewCLI(logger)
			phpcsPlugin := addon.NewUpdateCodingStyles(logger, phpcsRunner, config, composer)
			event.AddSubscriber(phpcsPlugin)
		}

		if !config.SkipRector {
			rectorRunner := rector.NewCLI(logger)
			rectorPlugin := addon.NewUpdateRemoveDeprecations(logger, rectorRunner, config, composer)
			event.AddSubscriber(rectorPlugin)
		}

		workflow.StartUpdate(ctx, strategy)
	},
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
		fmt.Println(err)
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
