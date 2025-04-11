package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"

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

	act.logger.Info("System", zap.Int("vcpu", runtime.NumCPU()), zap.String("architecture", runtime.GOARCH), zap.String("os", runtime.GOOS), zap.String("version", runtime.Version()), zap.Strings("env", os.Environ()))

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

func newCache() (otter.Cache[string, string], error) {
	return otter.MustBuilder[string, string](100).Build()
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
			newCache,
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

func main() {

	var rootCmd = &cobra.Command{
		Use:   "drupal-updater [config]",
		Short: "Drupal updater",
		Args:  cobra.ExactArgs(1),
		Run: func(_ *cobra.Command, args []string) {
			var config internal.Config
			err := json.Unmarshal([]byte(args[0]), &config)
			if err != nil {
				fmt.Println("Failed to parse config", err)
				os.Exit(1)
			}
			runApp(config)
		},
	}

	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
