package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/drupdater/drupdater/internal"
	"github.com/drupdater/drupdater/internal/codehosting"
	"github.com/drupdater/drupdater/internal/logging"
	"github.com/drupdater/drupdater/internal/report"
	"github.com/drupdater/drupdater/internal/services"
	"github.com/drupdater/drupdater/pkg/composer"
	"github.com/drupdater/drupdater/pkg/drupal"
	"github.com/drupdater/drupdater/pkg/drush"
	"github.com/drupdater/drupdater/pkg/repo"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

// checkFull selects the --full tier: on top of the free checks it clones the repository and runs
// composer install plus a real site install, proving each site installs from its exported config.
var checkFull bool

var checkCmd = &cobra.Command{
	Use:   "check",
	Short: "Validate prerequisites without running an update",
	Long: `Validates the things a real run depends on, without running composer update, without
creating a branch, and without opening a merge/pull request.

By default only cheap, near-instant checks run: .drupdater.yaml and its addon names, git
history, PHP platform requirements, each site's settings.php, and (if a token is given) that it
authenticates. Pass --full to additionally clone the repository and prove each site installs
from its exported configuration (drush site-install --existing-config) -- most of a real run's
cost, so it stays opt-in.

Exits non-zero if any check fails, so it can gate a pipeline.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cmd.SilenceUsage = true
		cmd.SilenceErrors = true

		redactor := logging.NewRedactor()
		registerEnvSecrets(redactor)
		logger, err := NewLogger(config, redactor)
		if err != nil {
			fmt.Fprintln(cmd.ErrOrStderr(), "failed to initialize logger:", err)
			return err
		}

		// A token is never required here: without one, the VCS authentication check is skipped.
		token := checkToken(args)
		redactor.Register(token)

		cfg := config
		gitSvc := repo.NewGitRepositoryService(logger)
		var resolveErr error
		if !cfg.Clone {
			resolveErr = resolveCheckoutSettings(gitSvc, &cfg)
		}

		composerCLI := composer.NewCLI(logger)
		defer composerCLI.Cleanup()

		ctx := cmd.Context()
		results := runCheapChecks(ctx, logger, configFilePath(configFile, cfg.WorkingDir), &cfg, gitSvc, composerCLI, afero.NewOsFs(), token, resolveErr)

		if checkFull {
			results = append(results, runFullChecks(ctx, logger, cfg, token)...)
		}

		printCheckResults(cmd.OutOrStdout(), results, redactor)

		writeCheckReport(logger, redactor, config.ReportPath,
			services.LookupToolVersions(ctx, logger, composerCLI), results)

		if anyCheckFailed(results) {
			return errors.New("preflight check failed")
		}
		return nil
	},
}

// cheapChecksComposer is the union of services.PlatformReqsChecker and
// services.ComposerConfigGetter. Kept narrow so tests can supply a small fake; the real
// *composer.CLI satisfies it as-is.
type cheapChecksComposer interface {
	services.PlatformReqsChecker
	services.ComposerConfigGetter
}

// runCheapChecks runs every check that's free: no composer install, no site install, no network
// beyond one optional read-only VCS call (see checkVCS). Split out of RunE so its branching can
// be tested against fakes, without the composer/drush/git binaries CI doesn't install.
func runCheapChecks(
	ctx context.Context,
	logger *zap.Logger,
	cfgFilePath string,
	cfg *internal.Config,
	repository services.ShallowCloneChecker,
	composerSvc cheapChecksComposer,
	fs afero.Fs,
	token string,
	resolveErr error,
) []services.CheckResult {
	var results []services.CheckResult
	results = append(results, checkConfigAndAddons(cfgFilePath, cfg)...)
	results = append(results, services.CheckGitHistoryComplete(repository, cfg.WorkingDir))
	results = append(results, services.CheckPlatformRequirements(ctx, composerSvc, cfg.WorkingDir))
	for _, site := range cfg.Sites {
		results = append(results, services.CheckSiteSettings(ctx, composerSvc, fs, cfg.WorkingDir, site))
	}
	results = append(results, checkVCS(ctx, logger, cfg.RepositoryURL, token, resolveErr)...)
	return results
}

// writeCheckReport writes the preflight result to path, or nothing without --report. A preflight
// has no phases, packages or branch, hence its own document shape. As with a run's report, a
// write failure is logged and swallowed: the verdict matters more than the file describing it.
func writeCheckReport(logger *zap.Logger, redactor *logging.Redactor, path string, tools report.ToolVersions, results []services.CheckResult) {
	if path == "" {
		return
	}

	check := report.NewCheck(internal.Version, tools, toReportCheckResults(results))
	if err := report.WriteCheck(afero.NewOsFs(), path, check, redactor.Redact); err != nil {
		logger.Warn("failed to write check report", zap.String("path", path), zap.Error(err))
		return
	}
	logger.Info("check report written", zap.String("path", path))
}

// toReportCheckResults converts the services results into the report's own schema type, for the
// same reason report.PackageChange mirrors composer.PackageChange.
func toReportCheckResults(results []services.CheckResult) []report.CheckResult {
	out := make([]report.CheckResult, 0, len(results))
	for _, r := range results {
		out = append(out, report.CheckResult{Name: r.Name, OK: r.OK, Detail: r.Detail})
	}

	return out
}

// checkToken resolves the token the way a real run does, but never errors when neither source is
// set: check works without one, it just can't verify authentication.
func checkToken(args []string) string {
	if len(args) == 1 && args[0] != "" {
		return args[0]
	}
	return os.Getenv("DRUPDATER_TOKEN")
}

// checkConfigAndAddons validates .drupdater.yaml and, if it parses, the addon names it lists.
// It also fills cfg.Sites (and the rest of the file config) for the checks that follow.
func checkConfigAndAddons(path string, cfg *internal.Config) []services.CheckResult {
	if _, err := internal.LoadConfigFile(path, cfg); err != nil {
		return []services.CheckResult{services.CheckFailed(".drupdater.yaml valid", err.Error())}
	}

	results := []services.CheckResult{
		services.CheckOK(fmt.Sprintf(".drupdater.yaml valid (sites: %s)", strings.Join(cfg.Sites, ", "))),
	}

	const addonsName = "addon names resolve"
	if err := validateAddons(*cfg); err != nil {
		return append(results, services.CheckFailed(addonsName, err.Error()))
	}
	return append(results, services.CheckOK(addonsName))
}

// newVcsProvider resolves a repository URL and token to a VCS platform. A variable so the token
// check can be tested without GetUser making a real network request.
var newVcsProvider = func(repositoryURL string, token string, logger *zap.Logger) (codehosting.Platform, error) {
	return codehosting.NewDefaultVcsProviderFactory().Create(repositoryURL, token, logger)
}

// checkVCS reports whether the repository URL routes to a known provider and, with a token,
// whether it authenticates. resolveErr comes from deriving the URL out of the checkout, and is
// surfaced here because this is the only check it would otherwise silently fail.
func checkVCS(ctx context.Context, logger *zap.Logger, repositoryURL string, token string, resolveErr error) []services.CheckResult {
	const name = "repository host recognized (GitHub/GitLab)"

	if repositoryURL == "" {
		detail := "could not determine repository URL (pass --repository-url or run inside a checkout with an origin remote)"
		if resolveErr != nil {
			detail = resolveErr.Error()
		}
		return []services.CheckResult{services.CheckFailed(name, detail)}
	}
	if err := codehosting.ValidateRepositoryURL(repositoryURL); err != nil {
		return []services.CheckResult{services.CheckFailed(name, err.Error())}
	}

	results := []services.CheckResult{services.CheckOK(name)}
	if token == "" {
		return results
	}

	const tokenCheckName = "token authenticates"
	platform, err := newVcsProvider(repositoryURL, token, logger)
	if err != nil {
		return append(results, services.CheckFailed(tokenCheckName, err.Error()))
	}
	userName, email := platform.GetUser(ctx)
	if userName == "" && email == "" {
		return append(results, services.CheckFailed(tokenCheckName, "did not authenticate, or lacks API access"))
	}
	return append(results, services.CheckOK(tokenCheckName))
}

// fullCheckComposer is what the --full tier needs from composer: the install it performs, the
// scratch-directory cleanup it owes, and the config lookup drupal.Installer makes.
type fullCheckComposer interface {
	drupal.Composer
	Install(ctx context.Context, path string) error
	Cleanup()
}

// siteInstaller is the one method the tier needs from drupal.Installer.
type siteInstaller interface {
	Install(ctx context.Context, dir string, site string) error
}

// fullCheckDeps are the external tools the --full tier drives. Constructors rather than values,
// so the tier keeps its ordering: composer is built only once the clone succeeded, the installer
// only once composer install did. Grouping them here makes the tier's control flow testable
// without a real clone, install and site install.
type fullCheckDeps struct {
	clone        func(repositoryURL string, branch string, token string) (string, error)
	newComposer  func() fullCheckComposer
	newInstaller func(fullCheckComposer) (siteInstaller, error)
}

// newFullCheckDeps builds the real services. It is a variable so tests can substitute doubles,
// the same way execCommand is swapped in the pkg/* wrappers.
var newFullCheckDeps = func(logger *zap.Logger) fullCheckDeps {
	return fullCheckDeps{
		clone: func(repositoryURL string, branch string, token string) (string, error) {
			_, _, path, err := repo.NewGitRepositoryService(logger).
				CloneRepository(repositoryURL, branch, token, "", "")
			return path, err
		},
		newComposer: func() fullCheckComposer { return composer.NewCLI(logger) },
		newInstaller: func(c fullCheckComposer) (siteInstaller, error) {
			cache, err := NewCache()
			if err != nil {
				return nil, err
			}
			return drupal.NewInstaller(logger, drush.NewCLI(logger, cache), c), nil
		},
	}
}

// runFullChecks is the --full tier: it clones to a scratch directory, never the live working
// copy, and proves each configured site installs from its exported configuration.
func runFullChecks(ctx context.Context, logger *zap.Logger, cfg internal.Config, token string) []services.CheckResult {
	if cfg.RepositoryURL == "" {
		return []services.CheckResult{services.CheckFailed("sites install from configuration",
			"no repository URL to clone (pass --repository-url or run inside a checkout "+
				"with an origin remote)")}
	}
	branch := cfg.Branch
	if branch == "" {
		branch = "main"
	}

	deps := newFullCheckDeps(logger)

	path, err := deps.clone(cfg.RepositoryURL, branch, token)
	if err != nil {
		return []services.CheckResult{services.CheckFailed("clone for full check", err.Error())}
	}
	defer cleanupFullCheckArtifacts(path, cfg.Sites)

	composerCLI := deps.newComposer()
	defer composerCLI.Cleanup()

	if err := composerCLI.Install(ctx, path); err != nil {
		return []services.CheckResult{services.CheckFailed("composer install", err.Error())}
	}
	results := []services.CheckResult{services.CheckOK("composer install")}

	installer, err := deps.newInstaller(composerCLI)
	if err != nil {
		return append(results, services.CheckFailed("drush site-install --existing-config", err.Error()))
	}

	for _, site := range cfg.Sites {
		name := fmt.Sprintf("site %q installs from configuration", site)
		if err := installer.Install(ctx, path, site); err != nil {
			results = append(results, services.CheckFailed(name, err.Error()))
			continue
		}
		results = append(results, services.CheckOK(name))
	}
	return results
}

// cleanupFullCheckArtifacts removes the clone and the SQLite databases/private files the site
// installs wrote beside it, mirroring the real run's own clone-mode cleanup.
func cleanupFullCheckArtifacts(path string, sites []string) {
	defer os.RemoveAll(path)

	services.CleanupSiteArtifacts(filepath.Dir(path), sites)
}

// printCheckResults writes results to w, redacting each detail first: a failed check's Detail
// can carry raw subprocess output, which would otherwise leak a credential straight to stdout,
// bypassing the logger's redaction entirely.
func printCheckResults(w io.Writer, results []services.CheckResult, redactor *logging.Redactor) {
	for _, r := range results {
		mark := "✓"
		if !r.OK {
			mark = "✗"
		}
		if !r.OK && r.Detail != "" {
			fmt.Fprintf(w, "%s %s: %s\n", mark, r.Name, redactor.Redact(r.Detail))
			continue
		}
		fmt.Fprintf(w, "%s %s\n", mark, r.Name)
	}
}

func anyCheckFailed(results []services.CheckResult) bool {
	return slices.ContainsFunc(results, func(r services.CheckResult) bool { return !r.OK })
}

func init() {
	checkCmd.Flags().BoolVar(&checkFull, "full", false,
		"Additionally clone the repository and verify each site installs from its exported "+
			"configuration (drush site-install --existing-config). Expensive: most of a real run's cost.")
	rootCmd.AddCommand(checkCmd)
}
