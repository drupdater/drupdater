package cmd

import (
	"context"
	"fmt"
	"os"

	"ebersolve.com/updater/internal"
	"ebersolve.com/updater/internal/codehosting"
	"ebersolve.com/updater/internal/services"
	"ebersolve.com/updater/internal/utils"

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
			return &fxevent.ZapLogger{Logger: log}
		}),
		fx.Provide(
			func() (*zap.Logger, error) {
				logger, err := zap.NewProduction()
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
				if config.UpdateStrategy == "Regular" {
					return newAction(lc, sh, logger, dependencyUpdateService)
				} else if config.UpdateStrategy == "Security" {
					return newAction(lc, sh, logger, securityUpdateService)
				}
				return nil
			},
		),
		fx.Options(services.Module, utils.Module, codehosting.Module),
		fx.Invoke(func(*Action) {}),
	)

	app.Run()
}

var config internal.Config

var rootCmd = &cobra.Command{
	Use:   "drupdater",
	Short: "Drupal Updater",
	Run: func(_ *cobra.Command, args []string) {
		runApp(config)
	},
}

func init() {
	rootCmd.PersistentFlags().StringVar(&config.RepositoryURL, "repository", "", "Repository URL")
	rootCmd.MarkPersistentFlagRequired("repository")
	rootCmd.PersistentFlags().StringVar(&config.Token, "token", "", "Token")
	rootCmd.MarkPersistentFlagRequired("token")
	rootCmd.PersistentFlags().StringVar(&config.Branch, "branch", "main", "Branch")
	rootCmd.PersistentFlags().StringArrayVar(&config.Sites, "sites", []string{"default"}, "Sites")
	rootCmd.PersistentFlags().StringVar(&config.UpdateStrategy, "update-strategy", "Regular", "Update strategy (Regular or Security)")
	rootCmd.PersistentFlags().BoolVar(&config.AutoMerge, "auto-merge", false, "Auto merge")
	rootCmd.PersistentFlags().BoolVar(&config.RunCBF, "run-cbf", false, "Run CBF")
	rootCmd.PersistentFlags().BoolVar(&config.RunRector, "run-rector", false, "Run Rector")
	rootCmd.PersistentFlags().BoolVar(&config.DryRun, "dry-run", false, "Dry run")
	rootCmd.PersistentFlags().BoolVar(&config.Verbose, "verbose", false, "Verbose")
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
