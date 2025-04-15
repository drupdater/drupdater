package cmd

import (
	"context"
	"fmt"
	"os"

	"drupdater/internal"
	"drupdater/internal/codehosting"
	"drupdater/internal/services"
	"drupdater/internal/utils"

	"github.com/maypok86/otter"
	"github.com/spf13/cobra"
	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"
	"go.uber.org/zap"
)

type Action struct {
	sh              fx.Shutdowner
	logger          *zap.Logger
	workflowService services.WorkflowService
}

func newAction(
	lc fx.Lifecycle,
	sh fx.Shutdowner,
	logger *zap.Logger,
	workflowService services.WorkflowService,
) *Action {
	act := &Action{
		sh:              sh,
		logger:          logger,
		workflowService: workflowService,
	}

	lc.Append(fx.Hook{
		OnStart: act.start,
		OnStop:  act.stop,
	})

	return act
}

func (act *Action) start(_ context.Context) error {
	go act.run()
	return nil
}

func (act *Action) stop(_ context.Context) error {
	fmt.Println("Stopped")
	return nil
}

func (act *Action) run() {
	exitCode := 0
	err := act.workflowService.StartUpdate()
	if err != nil {
		act.logger.Error("failed to start update", zap.Error(err))
		exitCode = 1
	}

	err = act.sh.Shutdown(fx.ExitCode(exitCode))
	if err != nil {
		act.logger.Error("failed to shutdown", zap.Error(err))
	}
}

func runApp(config internal.Config) {
	app := fx.New(
		fx.WithLogger(func(log *zap.Logger) fxevent.Logger {
			if config.Verbose {
				return &fxevent.ZapLogger{Logger: log}
			}
			return &fxevent.ZapLogger{Logger: zap.NewNop()}
		}),
		fx.Provide(
			func() (*zap.Logger, error) {
				logger, err := zap.NewDevelopment(
					zap.IncreaseLevel(zap.InfoLevel),
					zap.WithCaller(false),
				)
				if config.Verbose {
					logger, err = zap.NewDevelopment()
				}
				return logger, err
			},
			func() (otter.Cache[string, string], error) {
				return otter.MustBuilder[string, string](100).Build()
			},
			func() internal.Config {
				return config
			},
			func(lc fx.Lifecycle, sh fx.Shutdowner, logger *zap.Logger, dependencyUpdateService *services.WorkflowDependencyUpdateService, securityUpdateService *services.WorkflowSecurityUpdateService) *Action {
				if config.Security {
					return newAction(lc, sh, logger, securityUpdateService)
				}
				return newAction(lc, sh, logger, dependencyUpdateService)
			},
		),
		fx.Options(services.Module, utils.Module, codehosting.Module),
		fx.Invoke(func(*Action) {}),
	)

	app.Run()
}

var config internal.Config

var rootCmd = &cobra.Command{
	Use:   "drupdater <repository-url> <token>",
	Short: "Drupal Updater",
	Long:  `Drupal Updater is a tool to update Drupal dependencies and create merge requests.`,
	Args:  cobra.ExactArgs(2),
	Run: func(_ *cobra.Command, args []string) {
		config.RepositoryURL = args[0]
		config.Token = args[1]
		runApp(config)
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
