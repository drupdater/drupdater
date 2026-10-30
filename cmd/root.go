package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"

	"github.com/drupdater/drupdater/internal/codehosting"
	"github.com/drupdater/drupdater/internal/config"
	"github.com/drupdater/drupdater/internal/logging"
	"github.com/drupdater/drupdater/internal/report"
	"github.com/drupdater/drupdater/internal/workflow"
	"github.com/drupdater/drupdater/pkg/composer"
	"github.com/drupdater/drupdater/pkg/drupal"
	"github.com/drupdater/drupdater/pkg/drupalorg"
	"github.com/drupdater/drupdater/pkg/drush"
	"github.com/drupdater/drupdater/pkg/repo"
	"github.com/maypok86/otter"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// app holds everything the commands reach outside the process through; tests build their own.
type app struct {
	newRunner   func(logger *zap.Logger, cfg config.Config, s workflowServices, opts ...workflow.Option) updateRunner
	newPlatform func(repositoryURL string, token string, logger *zap.Logger) (codehosting.Platform, error)
	// newFullCheckDeps builds what the check command's --full tier clones and installs with.
	newFullCheckDeps func(logger *zap.Logger) fullCheckDeps
	// trustCheckout is the one write a run makes outside its checkout: git's global config.
	trustCheckout func(ctx context.Context, logger *zap.Logger, dir string)
	addons        addonCatalog
}

func productionApp() app {
	return app{
		newRunner: func(logger *zap.Logger, cfg config.Config, s workflowServices, opts ...workflow.Option) updateRunner {
			return workflow.New(logger, cfg, s.drush, s.platform, s.git, s.installer, s.composer, opts...)
		},
		newPlatform:      codehosting.New,
		newFullCheckDeps: realFullCheckDeps,
		trustCheckout:    ensureGitSafeDirectory,
		addons:           builtinAddons(),
	}
}

// workflowServices are what runUpdate builds for the workflow to run with.
type workflowServices struct {
	drush     workflow.Drush
	platform  codehosting.Platform
	git       workflow.GitService
	installer workflow.Installer
	composer  workflow.Composer
}

// options is what one command tree's flags set; a run gets a copy, so nothing carries over.
type options struct {
	config config.Config
	// configFile is the path to .drupdater.yaml; empty means <working-dir>/.drupdater.yaml.
	configFile string
	// checkFull adds the check command's expensive tier.
	checkFull bool
}

func (a app) newRootCmd() *cobra.Command {
	opts := &options{}
	root := &cobra.Command{
		Use:   "drupdater [token]",
		Short: "Drupal Updater",
		Long: `Drupal Updater is a tool to update Drupal dependencies and create merge requests.

The access token is read from the first argument, or from the DRUPDATER_TOKEN environment
variable when no argument is given (which keeps it out of the process list and shell history).

Project settings (sites, timeout, and which addons run) are read from .drupdater.yaml in the
working directory; override the path with --config. Run "drupdater addons" to list the addon
names you can set there. See the README for the full file format.`,
		Args: cobra.MaximumNArgs(1),
		PreRunE: func(*cobra.Command, []string) error {
			return validateFlags(opts.config)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			cmd.SilenceErrors = true

			return a.runUpdate(cmd, args, *opts)
		},
	}

	flags := root.PersistentFlags()
	flags.StringVar(&opts.config.Branch, "branch", "main", "Branch to update and target for the MR. Only used with --clone; in checkout mode it's taken from the checkout (or CI branch variable).")
	flags.StringVar(&opts.config.WorkingDir, "working-dir", ".", "Path to the existing checkout to update in place.")
	flags.BoolVar(&opts.config.Clone, "clone", false, "Clone the repository instead of using the existing checkout. Requires --repository-url. Intended for local testing.")
	flags.StringVar(&opts.config.RepositoryURL, "repository-url", "", "Repository URL. Required with --clone; otherwise derived from the checkout's origin remote.")
	flags.BoolVar(&opts.config.Security, "security", false, "Only security updates. If true, only security updates will be applied.")
	flags.BoolVar(&opts.config.DryRun, "dry-run", false, "Do not push the update branch or create a merge request. The branch and commits are still created locally.")
	flags.BoolVar(&opts.config.Verbose, "verbose", false, "Verbose")
	flags.StringVar(&opts.configFile, "config", "", "Path to the config file (default: <working-dir>/.drupdater.yaml).")
	// Equivalent mutant: GOMAXPROCS with any n < 1 only reads the setting, as 0 does.
	// mutator-disable-next-line numbers/decrementer
	flags.IntVar(&opts.config.Concurrency, "concurrency", runtime.GOMAXPROCS(0), "Maximum number of sites to install/update concurrently. Defaults to GOMAXPROCS(0), which reflects the container's CPU quota, not just the host's core count.")
	flags.StringVar(&opts.config.ReportPath, "report", "", "Write a machine-readable JSON report of the run to this path. Written on every outcome, including failures and --dry-run.")

	root.AddCommand(a.newAddonsCmd(), a.newCheckCmd(opts))
	return root
}

// validateFlags rejects flag combinations before anything runs.
func validateFlags(cfg config.Config) error {
	// --clone needs an explicit repository URL; checkout mode derives it from origin.
	if cfg.Clone && cfg.RepositoryURL == "" {
		return errors.New("--repository-url is required with --clone")
	}
	// Validated against what the provider factory accepts, so git@host:owner/repo.git passes.
	if cfg.RepositoryURL != "" {
		if err := codehosting.ValidateRepositoryURL(cfg.RepositoryURL); err != nil {
			return fmt.Errorf("invalid repository URL: %w", err)
		}
	}
	return nil
}

// runUpdate is the body of the root command's RunE. Named rather than inline because a closure
// assigned to RunE is invisible to `go tool cover -func`.
func (a app) runUpdate(cmd *cobra.Command, args []string, opts options) error {
	cfg := opts.config

	// Registered as each becomes known, so nothing is logged unredacted in between.
	redactor := logging.NewRedactor()
	registerEnvSecrets(redactor)

	// First, so config errors are reported at all — Cobra silences them.
	logger, err := NewLogger(cfg, redactor)
	if err != nil {
		// No logger yet.
		fmt.Fprintln(cmd.ErrOrStderr(), "failed to initialize logger:", err)
		return err
	}

	cfg.Token, err = resolveToken(args, cfg)
	if err != nil {
		logger.Error("missing token", zap.Error(err))
		return err
	}
	redactor.Register(cfg.Token)

	if err := loadProjectConfig(logger, a.addons, configFilePath(opts.configFile, cfg.WorkingDir), &cfg); err != nil {
		return err
	}

	cache, err := NewCache()
	if err != nil {
		logger.Error("failed to create cache", zap.Error(err))
		return err
	}

	drush := drush.NewCLI(logger, cache)
	composer := composer.NewCLI(logger, composerOptions(cfg)...)
	// Patch checks leave a scratch project with a full vendor tree behind otherwise.
	defer composer.Cleanup()
	drupalOrg := drupalorg.NewHTTPClient(logger)
	siteData, err := os.MkdirTemp("", "drupdater-sites-")
	if err != nil {
		logger.Error("failed to create the site database directory", zap.Error(err))
		return err
	}
	defer os.RemoveAll(siteData)
	installer := drupal.NewInstaller(logger, drush, composer, siteData)
	git := repo.NewGitRepositoryService(logger)

	// In checkout mode the URL and target branch come from the checkout; --branch is --clone only.
	if !cfg.Clone {
		if err := resolveCheckoutSettings(git, &cfg); err != nil {
			logger.Error("cannot use the checkout", zap.Error(err))
			return err
		}
		logger.Info("using checkout", zap.String("url", cfg.RepositoryURL), zap.String("branch", cfg.Branch))

		// CI mounts the checkout under another user, so git calls it "dubious ownership".
		a.trustCheckout(cmd.Context(), logger, cfg.WorkingDir)
	}

	// Built only for a run that clones or publishes; see tokenRequired.
	var platform codehosting.Platform
	if tokenRequired(cfg) {
		platform, err = a.newPlatform(cfg.RepositoryURL, cfg.Token, logger)
		if err != nil {
			logger.Error("failed to create VCS provider", zap.Error(err))
			return err
		}
	}

	addons, err := a.addons.create(cfg, newAddonDeps(logger, drush, composer, drupalOrg))
	if err != nil {
		return err
	}

	var runnerOpts []workflow.Option
	if cfg.ReportPath != "" {
		runnerOpts = append(runnerOpts, workflow.WithReportSink(reportSink(logger, redactor, cfg.ReportPath)))
	}
	runner := a.newRunner(logger, cfg, workflowServices{
		drush:     drush,
		platform:  platform,
		git:       git,
		installer: installer,
		composer:  composer,
	}, runnerOpts...)

	err = runner.Run(cmd.Context(), addons)
	if err != nil {
		if err := handleWorkflowError(logger, err); err != nil {
			return err
		}
	} else {
		logger.Info("update finished")
	}
	return nil
}

// updateRunner is narrow so a test can drive runUpdate without a real repository or subprocesses.
type updateRunner interface {
	Run(ctx context.Context, addons []workflow.Addon) error
}

// composerOptions lets a security run keep an unfixable advisory, reported as unresolved, rather
// than have it block every fixable one.
func composerOptions(cfg config.Config) []composer.Option {
	if cfg.Security {
		return []composer.Option{composer.WithoutBlocking()}
	}
	return nil
}

// reportSink writes the run report to path. A write failure is logged, never returned: on the
// failure path it would mask the real error.
func reportSink(logger *zap.Logger, redactor *logging.Redactor, path string) func(report.Report) {
	return func(rep report.Report) {
		if err := report.Write(afero.NewOsFs(), path, rep, redactor.Redact); err != nil {
			logger.Warn("failed to write run report", zap.String("path", path), zap.Error(err))
			return
		}
		logger.Info("run report written", zap.String("path", path))
	}
}

// configFilePath returns --config when set, otherwise the working directory's .drupdater.yaml.
func configFilePath(explicit string, workingDir string) string {
	if explicit != "" {
		return explicit
	}
	return filepath.Join(workingDir, ".drupdater.yaml")
}

// loadProjectConfig layers .drupdater.yaml onto cfg. A missing file is not an error.
func loadProjectConfig(logger *zap.Logger, catalog addonCatalog, path string, cfg *config.Config) error {
	found, err := config.LoadFile(path, cfg)
	if err != nil {
		logger.Error("invalid configuration", zap.String("path", path), zap.Error(err))
		return err
	}
	if err := catalog.validate(*cfg); err != nil {
		logger.Error("invalid configuration", zap.String("path", path), zap.Error(err))
		return err
	}
	logger.Debug("configuration loaded",
		zap.String("path", path),
		zap.Bool("file_found", found),
		zap.Strings("sites", cfg.Sites),
		zap.Duration("timeout", cfg.Timeout),
		zap.Strings("run_types.normal.addons", cfg.RunTypes.Normal.Addons),
		zap.Bool("run_types.normal.auto_merge", cfg.RunTypes.Normal.AutoMerge),
		zap.Strings("run_types.security.addons", cfg.RunTypes.Security.Addons),
		zap.Bool("run_types.security.auto_merge", cfg.RunTypes.Security.AutoMerge),
	)
	return nil
}

// handleWorkflowError logs AbortErrors as warnings (non-fatal) and all others as errors (fatal).
func handleWorkflowError(logger *zap.Logger, err error) error {
	if errors.As(err, &workflow.AbortError{}) {
		logger.Warn("update aborted", zap.Error(err))
		return nil
	}
	logger.Error("update failed", zap.Error(err))
	return err
}

func Execute() {
	// Cancel the workflow context on SIGINT/SIGTERM so cleanup runs on termination.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := execute(ctx, productionApp().newRootCmd())
	stop()
	os.Exit(code)
}

// execute runs cmd and returns the process's exit status, which is what a CI pipeline reads.
func execute(ctx context.Context, cmd *cobra.Command) int {
	if err := cmd.ExecuteContext(ctx); err != nil {
		return 1
	}
	return 0
}

func NewCache() (otter.Cache[string, string], error) {
	return otter.MustBuilder[string, string](100).Build()
}

func NewLogger(cfg config.Config, redactor *logging.Redactor) (*zap.Logger, error) {
	loggerConfig := zap.NewDevelopmentConfig()
	loggerConfig.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder

	if !cfg.Verbose {
		loggerConfig.Level.SetLevel(zapcore.InfoLevel)
		loggerConfig.DisableCaller = true
		loggerConfig.DisableStacktrace = true
	}
	return loggerConfig.Build(zap.AddStacktrace(zapcore.ErrorLevel), zap.WrapCore(logging.WrapCore(redactor)))
}
