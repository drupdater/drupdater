package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/drupdater/drupdater/internal/buildinfo"
	"github.com/drupdater/drupdater/internal/codehosting"
	"github.com/drupdater/drupdater/internal/config"
	"github.com/drupdater/drupdater/internal/logging"
	"github.com/drupdater/drupdater/internal/report"
	"github.com/drupdater/drupdater/internal/workflow"
	"github.com/drupdater/drupdater/pkg/composer"
	"github.com/drupdater/drupdater/pkg/drupal"
	"github.com/drupdater/drupdater/pkg/drush"
	"github.com/drupdater/drupdater/pkg/repo"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

// newCheckCmd shares opts with the root command, whose persistent flags it reads.
func (a app) newCheckCmd(opts *options) *cobra.Command {
	check := &cobra.Command{
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

			return a.runCheck(cmd, args, *opts)
		},
	}
	check.Flags().BoolVar(&opts.checkFull, "full", false,
		"Additionally clone the repository and verify each site installs from its exported "+
			"configuration (drush site-install --existing-config). Expensive: most of a real run's cost.")
	return check
}

// runCheck is the check command's RunE; opts.checkFull adds the clone and the site installs.
func (a app) runCheck(cmd *cobra.Command, args []string, opts options) error {
	cfg := opts.config
	redactor := logging.NewRedactor()
	registerEnvSecrets(redactor)
	logger, err := NewLogger(cfg, redactor)
	if err != nil {
		fmt.Fprintln(cmd.ErrOrStderr(), "failed to initialize logger:", err)
		return err
	}

	// Optional: without a token the VCS authentication check is skipped.
	token := checkToken(args)
	redactor.Register(token)

	gitSvc := repo.NewGitRepositoryService(logger)
	var resolveErr error
	if !cfg.Clone {
		resolveErr = resolveCheckoutSettings(gitSvc, &cfg)
	}

	composerCLI := composer.NewCLI(logger)
	defer composerCLI.Cleanup()

	ctx := cmd.Context()
	results := a.runCheapChecks(ctx, logger, configFilePath(opts.configFile, cfg.WorkingDir), &cfg, gitSvc, composerCLI, afero.NewOsFs(), token, resolveErr)

	if opts.checkFull {
		results = append(results, a.runFullChecks(ctx, logger, cfg, token)...)
	}

	printCheckResults(cmd.OutOrStdout(), results, redactor)

	writeCheckReport(logger, redactor, cfg.ReportPath,
		workflow.LookupToolVersions(ctx, logger, composerCLI), results)

	if anyCheckFailed(results) {
		return errors.New("preflight check failed")
	}
	return nil
}

// cheapChecksComposer is narrow so tests can supply a fake; *composer.CLI satisfies it as-is.
type cheapChecksComposer interface {
	workflow.PlatformReqsChecker
	workflow.ComposerConfigGetter
}

// runCheapChecks runs the free checks: no install, no network beyond one optional VCS call.
func (a app) runCheapChecks(
	ctx context.Context,
	logger *zap.Logger,
	cfgFilePath string,
	cfg *config.Config,
	repository workflow.ShallowCloneChecker,
	composerSvc cheapChecksComposer,
	fs afero.Fs,
	token string,
	resolveErr error,
) []workflow.CheckResult {
	var results []workflow.CheckResult
	results = append(results, checkConfigAndAddons(a.addons, cfgFilePath, cfg)...)
	results = append(results, workflow.CheckGitHistoryComplete(repository, cfg.WorkingDir))
	results = append(results, workflow.CheckPlatformRequirements(ctx, composerSvc, cfg.WorkingDir))
	for _, site := range cfg.Sites {
		results = append(results, workflow.CheckSiteSettings(ctx, composerSvc, fs, cfg.WorkingDir, site))
	}
	results = append(results, a.checkVCS(ctx, logger, cfg.RepositoryURL, token, resolveErr)...)
	return results
}

// writeCheckReport writes the preflight result, or nothing without --report. Failures are logged.
func writeCheckReport(logger *zap.Logger, redactor *logging.Redactor, path string, tools report.ToolVersions, results []workflow.CheckResult) {
	if path == "" {
		return
	}

	check := report.NewCheck(buildinfo.Version, tools, toReportCheckResults(results))
	if err := report.WriteCheck(afero.NewOsFs(), path, check, redactor.Redact); err != nil {
		logger.Warn("failed to write check report", zap.String("path", path), zap.Error(err))
		return
	}
	logger.Info("check report written", zap.String("path", path))
}

// toReportCheckResults converts to the report's own schema type — see report.PackageChange.
func toReportCheckResults(results []workflow.CheckResult) []report.CheckResult {
	out := make([]report.CheckResult, 0, len(results))
	for _, r := range results {
		out = append(out, report.CheckResult{Name: r.Name, OK: r.OK, Detail: r.Detail})
	}

	return out
}

// checkToken resolves the token like a real run does, but check works without one.
func checkToken(args []string) string {
	if len(args) == 1 && args[0] != "" {
		return args[0]
	}
	return os.Getenv("DRUPDATER_TOKEN")
}

// checkConfigAndAddons validates .drupdater.yaml and its addon names, and fills cfg for the rest.
func checkConfigAndAddons(catalog addonCatalog, path string, cfg *config.Config) []workflow.CheckResult {
	if _, err := config.LoadFile(path, cfg); err != nil {
		return []workflow.CheckResult{workflow.CheckFailed(".drupdater.yaml valid", err.Error())}
	}

	results := []workflow.CheckResult{
		workflow.CheckOK(fmt.Sprintf(".drupdater.yaml valid (sites: %s)", strings.Join(cfg.Sites, ", "))),
	}

	const addonsName = "addon names resolve"
	if err := catalog.validate(*cfg); err != nil {
		return append(results, workflow.CheckFailed(addonsName, err.Error()))
	}
	return append(results, workflow.CheckOK(addonsName))
}

// checkVCS reports whether the URL routes to a known provider and, with a token, authenticates.
// resolveErr is surfaced here because this is the only check it would otherwise silently fail.
func (a app) checkVCS(ctx context.Context, logger *zap.Logger, repositoryURL string, token string, resolveErr error) []workflow.CheckResult {
	const name = "repository host recognized (GitHub/GitLab)"

	if repositoryURL == "" {
		detail := "could not determine repository URL (pass --repository-url or run inside a checkout with an origin remote)"
		if resolveErr != nil {
			detail = resolveErr.Error()
		}
		return []workflow.CheckResult{workflow.CheckFailed(name, detail)}
	}
	if err := codehosting.ValidateRepositoryURL(repositoryURL); err != nil {
		return []workflow.CheckResult{workflow.CheckFailed(name, err.Error())}
	}

	results := []workflow.CheckResult{workflow.CheckOK(name)}
	if token == "" {
		return results
	}

	const tokenCheckName = "token authenticates"
	platform, err := a.newPlatform(repositoryURL, token, logger)
	if err != nil {
		return append(results, workflow.CheckFailed(tokenCheckName, err.Error()))
	}
	userName, email := platform.GetUser(ctx)
	if userName == "" && email == "" {
		return append(results, workflow.CheckFailed(tokenCheckName, "did not authenticate, or lacks API access"))
	}
	return append(results, workflow.CheckOK(tokenCheckName))
}

// fullCheckComposer is what the --full tier needs from composer.
type fullCheckComposer interface {
	drupal.Composer
	Install(ctx context.Context, path string) error
	Cleanup()
}

// siteInstaller is the one method the tier needs from drupal.Installer.
type siteInstaller interface {
	Install(ctx context.Context, dir string, site string) error
}

// fullCheckDeps holds constructors rather than values, so the tier keeps its build order:
// composer only once the clone succeeded, the installer only once composer install did.
type fullCheckDeps struct {
	clone        func(repositoryURL string, branch string, token string) (string, error)
	newComposer  func() fullCheckComposer
	newInstaller func(c fullCheckComposer, dataDir string) (siteInstaller, error)
}

// realFullCheckDeps builds the dependencies that really clone and install.
func realFullCheckDeps(logger *zap.Logger) fullCheckDeps {
	return fullCheckDeps{
		clone: func(repositoryURL string, branch string, token string) (string, error) {
			checkout, err := repo.NewGitRepositoryService(logger).
				CloneRepository(repositoryURL, branch, token, "", "")
			return checkout.Path, err
		},
		newComposer: func() fullCheckComposer { return composer.NewCLI(logger) },
		newInstaller: func(c fullCheckComposer, dataDir string) (siteInstaller, error) {
			cache, err := NewCache()
			if err != nil {
				return nil, err
			}
			return drupal.NewInstaller(logger, drush.NewCLI(logger, cache), c, dataDir), nil
		},
	}
}

// runFullChecks clones to a scratch directory, never the live working copy, and installs each site.
func (a app) runFullChecks(ctx context.Context, logger *zap.Logger, cfg config.Config, token string) []workflow.CheckResult {
	if cfg.RepositoryURL == "" {
		return []workflow.CheckResult{workflow.CheckFailed("sites install from configuration",
			"no repository URL to clone (pass --repository-url or run inside a checkout "+
				"with an origin remote)")}
	}
	branch := cfg.Branch
	if branch == "" {
		branch = "main"
	}

	deps := a.newFullCheckDeps(logger)

	path, err := deps.clone(cfg.RepositoryURL, branch, token)
	if err != nil {
		return []workflow.CheckResult{workflow.CheckFailed("clone for full check", err.Error())}
	}
	defer os.RemoveAll(path)

	composerCLI := deps.newComposer()
	defer composerCLI.Cleanup()

	if err := composerCLI.Install(ctx, path); err != nil {
		return []workflow.CheckResult{workflow.CheckFailed("composer install", err.Error())}
	}
	results := []workflow.CheckResult{workflow.CheckOK("composer install")}

	siteData, err := os.MkdirTemp("", "drupdater-sites-")
	if err != nil {
		return append(results, workflow.CheckFailed("drush site-install --existing-config", err.Error()))
	}
	defer os.RemoveAll(siteData)

	installer, err := deps.newInstaller(composerCLI, siteData)
	if err != nil {
		return append(results, workflow.CheckFailed("drush site-install --existing-config", err.Error()))
	}

	for _, site := range cfg.Sites {
		name := fmt.Sprintf("site %q installs from configuration", site)
		if err := installer.Install(ctx, path, site); err != nil {
			results = append(results, workflow.CheckFailed(name, err.Error()))
			continue
		}
		results = append(results, workflow.CheckOK(name))
	}
	return results
}

// printCheckResults redacts each detail: a failure's Detail can carry raw subprocess output.
func printCheckResults(w io.Writer, results []workflow.CheckResult, redactor *logging.Redactor) {
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

func anyCheckFailed(results []workflow.CheckResult) bool {
	return slices.ContainsFunc(results, func(r workflow.CheckResult) bool { return !r.OK })
}
