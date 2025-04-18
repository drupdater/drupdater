package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/drupdater/drupdater/internal"
	"github.com/drupdater/drupdater/internal/codehosting"
	"github.com/drupdater/drupdater/internal/services"
	"github.com/drupdater/drupdater/internal/utils"
	"github.com/drupdater/drupdater/pkg"

	"github.com/maypok86/otter"
	"github.com/spf13/cobra"
	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"
	"go.uber.org/zap"
)

type Action struct {
	sh               fx.Shutdowner
	logger           *zap.Logger
	workflowService  services.WorkflowService
	workflowStrategy services.WorkflowStrategy
}

func newAction(
	lc fx.Lifecycle,
	sh fx.Shutdowner,
	logger *zap.Logger,
	workflowService services.WorkflowService,
	workflowStrategy services.WorkflowStrategy,
) *Action {
	act := &Action{
		sh:               sh,
		logger:           logger,
		workflowService:  workflowService,
		workflowStrategy: workflowStrategy,
	}

	lc.Append(fx.Hook{
		OnStart: act.start,
		OnStop:  act.stop,
	})

	return act
}

func (act *Action) start(ctx context.Context) error {
	go act.run(ctx)
	return nil
}

func (act *Action) stop(_ context.Context) error {
	return nil
}

func (act *Action) run(ctx context.Context) {

	exitCode := 0
	err := act.workflowService.StartUpdate(ctx, act.workflowStrategy)
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
					zap.AddStacktrace(zap.PanicLevel),
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
			func(lc fx.Lifecycle, sh fx.Shutdowner, logger *zap.Logger, workflowService services.WorkflowService, dependencyUpdateService services.DependencyUpdateStrategy, securityUpdateService services.SecurityUpdateStrategy) *Action {
				if config.Security {
					return newAction(lc, sh, logger, workflowService, securityUpdateService)
				}
				return newAction(lc, sh, logger, workflowService, dependencyUpdateService)
			},
		),
		fx.Options(services.Module, utils.Module, codehosting.Module, pkg.Module),
		fx.Invoke(func(*Action) {}),
		fx.StartTimeout(15*time.Minute),
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
