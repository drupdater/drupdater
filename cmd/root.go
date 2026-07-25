package cmd

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"

	"github.com/drupdater/drupdater/internal"
	"github.com/drupdater/drupdater/internal/addon"
	"github.com/drupdater/drupdater/internal/codehosting"
	"github.com/drupdater/drupdater/internal/logging"
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
		// Validate the URL format when one is given, against what the provider factory accepts
		// (HTTP(S) and SCP-style git URLs) rather than a stricter URL parser that would reject
		// git@host:owner/repo.git even though cloning and provider detection handle it.
		if config.RepositoryURL != "" {
			if err := codehosting.ValidateRepositoryURL(config.RepositoryURL); err != nil {
				return fmt.Errorf("invalid repository URL: %w", err)
			}
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		// Silence default error handling
		cmd.SilenceUsage = true
		cmd.SilenceErrors = true

		// Values subprocesses (Composer, Drush, git) can echo back in their own output — most
		// often a credential embedded in a URL after a failed authenticated fetch. Registered
		// as soon as each becomes known so nothing is logged unredacted in between; never
		// logged itself, including at debug level with --verbose.
		redactor := logging.NewRedactor()
		redactor.Register(os.Getenv("DRUPALCODE_ACCESS_TOKEN"))
		registerComposerAuth(redactor, os.Getenv("COMPOSER_AUTH"))

		// Initialize the logger first so config errors are reported (errors are silenced by Cobra).
		logger, err := NewLogger(config, redactor)
		if err != nil {
			// No logger yet, so this one report has to go straight to stderr.
			fmt.Fprintln(cmd.ErrOrStderr(), "failed to initialize logger:", err)
			return err
		}

		config.Token, err = resolveToken(args, config)
		if err != nil {
			logger.Error("missing token", zap.Error(err))
			return err
		}
		redactor.Register(config.Token)

		// Load per-project config from .drupdater.yaml (sites, timeout, addons). A missing file
		// falls back to built-in defaults.
		if err := loadProjectConfig(logger, configFilePath(configFile, config.WorkingDir), &config); err != nil {
			return err
		}

		cache, err := NewCache()
		if err != nil {
			logger.Error("failed to create cache", zap.Error(err))
			return err
		}

		// Create core service instances
		drush := drush.NewCLI(logger, cache)
		composer := composer.NewCLI(logger)
		// Patch-apply checks build a scratch composer project in a temp directory; drop it
		// when the run ends rather than leaving a vendor tree behind on every invocation.
		defer composer.Cleanup()
		drupalOrg := drupalorg.NewHTTPClient(logger)
		installer := drupal.NewInstaller(logger, drush, composer)
		git := repo.NewGitRepositoryService(logger)

		// In checkout mode the repository URL and target branch come from the checkout, so
		// they don't have to be passed in. --branch only applies to --clone.
		if !config.Clone {
			if err := resolveCheckoutSettings(git, &config); err != nil {
				return err
			}
			logger.Info("using checkout", zap.String("url", config.RepositoryURL), zap.String("branch", config.Branch))

			// CI mounts the checkout owned by a different user than the container runs as, so
			// the git binary (invoked by drush/composer) refuses it as "dubious ownership".
			// Mark it safe so those child processes can run git against it.
			ensureGitSafeDirectory(cmd.Context(), logger, config.WorkingDir)
		}

		// Only construct the VCS platform when a run will actually use it: cloning (may be
		// authenticated) or publishing (pushing + creating the MR, i.e. anything without
		// --dry-run). A checkout-mode --dry-run run does neither, so it needs no VCS client and
		// no token — see tokenRequired.
		var platform codehosting.Platform
		if tokenRequired(config) {
			vcsProviderFactory := codehosting.NewDefaultVcsProviderFactory()
			platform, err = vcsProviderFactory.Create(config.RepositoryURL, config.Token, logger)
			if err != nil {
				logger.Error("failed to create VCS provider", zap.Error(err))
				return err
			}
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

// resolveToken returns the access token: the positional argument when one is given, otherwise
// DRUPDATER_TOKEN. The environment variable is the preferred form because it keeps the token
// out of the process list and the shell history.
//
// The token is only mandatory when the run will use it — see tokenRequired. A checkout-mode
// --dry-run run pushes nothing and creates no MR, so it can proceed with no token and no
// DRUPDATER_TOKEN set at all.
func resolveToken(args []string, cfg internal.Config) (string, error) {
	var token string
	if len(args) == 1 && args[0] != "" {
		token = args[0]
	} else {
		token = os.Getenv("DRUPDATER_TOKEN")
	}
	if token == "" && tokenRequired(cfg) {
		return "", errors.New("no token provided: pass it as the argument or set DRUPDATER_TOKEN")
	}
	return token, nil
}

// tokenRequired reports whether this run performs an operation that needs VCS credentials:
// cloning (--clone, which may be authenticated) or publishing (pushing the update branch and
// creating the merge/pull request — i.e. any run without --dry-run). A checkout-mode --dry-run
// run does neither.
func tokenRequired(cfg internal.Config) bool {
	return cfg.Clone || !cfg.DryRun
}

// registerComposerAuth registers the credentials carried by a COMPOSER_AUTH env value with the
// redactor. COMPOSER_AUTH is a JSON object (http-basic/bearer/gitlab-token/github-oauth/... per
// host, see the Composer docs) and Composer echoes the individual username, password, or token
// value it contains — typically embedded in a URL after a failed authenticated fetch — not the
// blob itself, so registering only the raw string would never match that output. Every string
// leaf of the parsed JSON is registered individually; the raw value is registered too, as a
// fallback for a value that fails to parse as JSON.
func registerComposerAuth(redactor *logging.Redactor, composerAuth string) {
	redactor.Register(composerAuth)

	var parsed any
	if err := json.Unmarshal([]byte(composerAuth), &parsed); err != nil {
		return
	}
	redactor.Register(jsonStringLeaves(parsed)...)
}

// jsonStringLeaves returns every string value found anywhere in a decoded JSON structure
// (as produced by json.Unmarshal into `any`), recursing through nested objects and arrays.
func jsonStringLeaves(v any) []string {
	switch t := v.(type) {
	case string:
		return []string{t}
	case map[string]any:
		leaves := make([]string, 0, len(t))
		for _, val := range t {
			leaves = append(leaves, jsonStringLeaves(val)...)
		}
		return leaves
	case []any:
		leaves := make([]string, 0, len(t))
		for _, val := range t {
			leaves = append(leaves, jsonStringLeaves(val)...)
		}
		return leaves
	default:
		return nil
	}
}

// configFilePath returns the .drupdater.yaml to read: --config when set, otherwise the one in
// the working directory.
func configFilePath(explicit string, workingDir string) string {
	if explicit != "" {
		return explicit
	}
	return filepath.Join(workingDir, ".drupdater.yaml")
}

// loadProjectConfig layers the project's .drupdater.yaml onto cfg and validates the addon
// names it lists. A missing file is not an error — the built-in defaults apply.
func loadProjectConfig(logger *zap.Logger, path string, cfg *internal.Config) error {
	found, err := internal.LoadConfigFile(path, cfg)
	if err != nil {
		logger.Error("invalid configuration", zap.String("path", path), zap.Error(err))
		return err
	}
	if err := validateAddons(*cfg); err != nil {
		logger.Error("invalid configuration", zap.String("path", path), zap.Error(err))
		return err
	}
	logger.Debug("configuration loaded",
		zap.String("path", path),
		zap.Bool("file_found", found),
		zap.Strings("sites", cfg.Sites),
		zap.Duration("timeout", cfg.Timeout),
		zap.Strings("addons.normal", cfg.Addons.Normal),
		zap.Strings("addons.security", cfg.Addons.Security),
	)
	return nil
}

// resolveCheckoutSettings fills in the repository URL and target branch from the checkout,
// which is where checkout mode takes them from instead of flags. --branch only applies to
// --clone, so it is overwritten here.
func resolveCheckoutSettings(git *repo.GitRepositoryService, cfg *internal.Config) error {
	if cfg.RepositoryURL == "" {
		remoteURL, err := git.GetRemoteURL(cfg.WorkingDir)
		if err != nil {
			return fmt.Errorf("failed to determine repository URL from checkout (pass --repository-url or run inside a checkout): %w", err)
		}
		cfg.RepositoryURL = remoteURL
	}

	branch, err := resolveCheckoutBranch(git, cfg.WorkingDir)
	if err != nil {
		return err
	}
	cfg.Branch = branch
	return nil
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
func ensureGitSafeDirectory(ctx context.Context, logger *zap.Logger, dir string) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		logger.Warn("failed to resolve checkout path for safe.directory", zap.Error(err))
		return
	}

	if out, err := exec.CommandContext(ctx, "git", "config", "--global", "--get-all", "safe.directory").Output(); err == nil {
		for _, entry := range strings.Split(string(out), "\n") {
			if entry == abs || entry == "*" {
				return
			}
		}
	}

	if out, err := exec.CommandContext(ctx, "git", "config", "--global", "--add", "safe.directory", abs).CombinedOutput(); err != nil {
		logger.Warn("failed to mark checkout as a safe git directory", zap.String("output", string(out)), zap.Error(err))
	}
}

// addonDeps carries everything the addon constructors need.
type addonDeps struct {
	logger    *zap.Logger
	drush     addon.Drush
	composer  addon.Composer
	drupalOrg addon.DrupalOrg
	git       addon.Repository
}

// addonRegistry maps the names used in .drupdater.yaml to their constructors.
var addonRegistry = map[string]func(addonDeps) internal.Addon{
	"composer_audit": func(d addonDeps) internal.Addon { return addon.NewComposerAudit(d.logger, d.composer) },
	"code_beautifier": func(d addonDeps) internal.Addon {
		return addon.NewCodeBeautifier(d.logger, phpcs.NewCLI(d.logger), d.composer)
	},
	"deprecations_remover": func(d addonDeps) internal.Addon {
		return addon.NewDeprecationsRemover(d.logger, rector.NewCLI(d.logger), d.composer)
	},
	"translations_updater":   func(d addonDeps) internal.Addon { return addon.NewTranslationsUpdater(d.logger, d.drush, d.git) },
	"composer_allow_plugins": func(d addonDeps) internal.Addon { return addon.NewComposerAllowPlugins(d.logger, d.composer) },
	"composer_normalizer":    func(d addonDeps) internal.Addon { return addon.NewComposerNormalizer(d.logger, d.composer) },
	"composer_patches": func(d addonDeps) internal.Addon {
		return addon.NewComposerPatches1(d.logger, d.composer, d.drupalOrg, http.DefaultClient)
	},
	"composer_diff":       func(d addonDeps) internal.Addon { return addon.NewComposerDiff(d.logger, d.composer) },
	"update_hooks":        func(d addonDeps) internal.Addon { return addon.NewUpdateHooks(d.logger, d.drush) },
	"unsupported_modules": func(d addonDeps) internal.Addon { return addon.NewUnsupportedModules(d.logger, d.drush) },
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
	deps := addonDeps{logger: logger, drush: drush, composer: composer, drupalOrg: drupalOrg, git: git}

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
	rootCmd.PersistentFlags().BoolVar(&config.DryRun, "dry-run", false, "Do not push the update branch or create a merge request. The branch and commits are still created locally.")
	rootCmd.PersistentFlags().BoolVar(&config.Verbose, "verbose", false, "Verbose")
	rootCmd.PersistentFlags().StringVar(&configFile, "config", "", "Path to the config file (default: <working-dir>/.drupdater.yaml).")
	rootCmd.PersistentFlags().IntVar(&config.Concurrency, "concurrency", runtime.GOMAXPROCS(0), "Maximum number of sites to install/update concurrently. Defaults to GOMAXPROCS(0), which reflects the container's CPU quota, not just the host's core count.")

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

func NewCache() (otter.Cache[string, string], error) {
	return otter.MustBuilder[string, string](100).Build()
}

func NewLogger(config internal.Config, redactor *logging.Redactor) (*zap.Logger, error) {
	loggerConfig := zap.NewDevelopmentConfig()
	loggerConfig.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder

	if !config.Verbose {
		loggerConfig.Level.SetLevel(zapcore.InfoLevel)
		loggerConfig.DisableCaller = true
		loggerConfig.DisableStacktrace = true
	}
	return loggerConfig.Build(zap.AddStacktrace(zapcore.ErrorLevel), zap.WrapCore(logging.WrapCore(redactor)))
}
