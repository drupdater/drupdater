package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/drupdater/drupdater/internal"
	"github.com/drupdater/drupdater/internal/codehosting"
	"github.com/drupdater/drupdater/internal/logging"
	"github.com/drupdater/drupdater/internal/services"
	"github.com/drupdater/drupdater/pkg/composer"
	"github.com/drupdater/drupdater/pkg/drupal"
	"github.com/drupdater/drupdater/pkg/drush"
	"github.com/drupdater/drupdater/pkg/repo"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

// checkFull selects the --full tier of "drupdater check": on top of the free checks, it clones
// the repository and runs composer install plus a real site install for each configured site,
// proving the site actually installs from its exported configuration.
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
		logger, err := NewLogger(config, redactor)
		if err != nil {
			fmt.Fprintln(cmd.ErrOrStderr(), "failed to initialize logger:", err)
			return err
		}

		// Unlike a real run, a token is never required here: it only sharpens one check (whether
		// the given token authenticates to the VCS host) and its absence just skips that check.
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
		var results []services.CheckResult
		results = append(results, checkConfigAndAddons(configFilePath(configFile, cfg.WorkingDir), &cfg)...)
		results = append(results, services.CheckGitHistoryComplete(gitSvc, cfg.WorkingDir))
		results = append(results, services.CheckPlatformRequirements(ctx, composerCLI, cfg.WorkingDir))
		for _, site := range cfg.Sites {
			results = append(results, services.CheckSiteSettings(ctx, composerCLI, afero.NewOsFs(), cfg.WorkingDir, site))
		}
		results = append(results, checkVCS(ctx, logger, cfg.RepositoryURL, token, resolveErr)...)

		if checkFull {
			results = append(results, runFullChecks(ctx, logger, cfg, token)...)
		}

		printCheckResults(cmd.OutOrStdout(), results)
		if anyCheckFailed(results) {
			return errors.New("preflight check failed")
		}
		return nil
	},
}

// checkToken resolves the access token the same way the real run does (positional argument,
// falling back to DRUPDATER_TOKEN), but never errors when neither is set: check works fine
// without one, it just can't verify that a given token authenticates.
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
		return []services.CheckResult{{Name: ".drupdater.yaml valid", OK: false, Detail: err.Error()}}
	}

	results := []services.CheckResult{{
		Name: fmt.Sprintf(".drupdater.yaml valid (sites: %s)", strings.Join(cfg.Sites, ", ")),
		OK:   true,
	}}

	if err := validateAddons(*cfg); err != nil {
		return append(results, services.CheckResult{Name: "addon names resolve", OK: false, Detail: err.Error()})
	}
	return append(results, services.CheckResult{Name: "addon names resolve", OK: true})
}

// checkVCS reports whether the repository URL routes to a known VCS provider (GitHub or
// GitLab) and, when a token is given, whether it authenticates. resolveErr is the error (if
// any) from deriving the repository URL out of the checkout, surfaced here since that's the
// only check it would otherwise silently fail.
func checkVCS(ctx context.Context, logger *zap.Logger, repositoryURL string, token string, resolveErr error) []services.CheckResult {
	const name = "repository host recognized (GitHub/GitLab)"

	if repositoryURL == "" {
		detail := "could not determine repository URL (pass --repository-url or run inside a checkout with an origin remote)"
		if resolveErr != nil {
			detail = resolveErr.Error()
		}
		return []services.CheckResult{{Name: name, OK: false, Detail: detail}}
	}
	if err := codehosting.ValidateRepositoryURL(repositoryURL); err != nil {
		return []services.CheckResult{{Name: name, OK: false, Detail: err.Error()}}
	}

	results := []services.CheckResult{{Name: name, OK: true}}
	if token == "" {
		return results
	}

	const tokenCheckName = "token authenticates"
	platform, err := codehosting.NewDefaultVcsProviderFactory().Create(repositoryURL, token, logger)
	if err != nil {
		return append(results, services.CheckResult{Name: tokenCheckName, OK: false, Detail: err.Error()})
	}
	userName, email := platform.GetUser(ctx)
	if userName == "" && email == "" {
		return append(results, services.CheckResult{Name: tokenCheckName, OK: false, Detail: "did not authenticate, or lacks API access"})
	}
	return append(results, services.CheckResult{Name: tokenCheckName, OK: true})
}

// runFullChecks is the --full tier: it clones the repository to a scratch directory (never the
// live working copy, so the check has no side effects on it) and proves each configured site
// installs from its exported configuration.
func runFullChecks(ctx context.Context, logger *zap.Logger, cfg internal.Config, token string) []services.CheckResult {
	if cfg.RepositoryURL == "" {
		return []services.CheckResult{{
			Name: "sites install from configuration",
			OK:   false,
			Detail: "no repository URL to clone (pass --repository-url or run inside a checkout " +
				"with an origin remote)",
		}}
	}
	branch := cfg.Branch
	if branch == "" {
		branch = "main"
	}

	gitSvc := repo.NewGitRepositoryService(logger)
	_, _, path, err := gitSvc.CloneRepository(cfg.RepositoryURL, branch, token, "", "")
	if err != nil {
		return []services.CheckResult{{Name: "clone for full check", OK: false, Detail: err.Error()}}
	}
	defer cleanupFullCheckArtifacts(path, cfg.Sites)

	composerCLI := composer.NewCLI(logger)
	defer composerCLI.Cleanup()

	if err := composerCLI.Install(ctx, path); err != nil {
		return []services.CheckResult{{Name: "composer install", OK: false, Detail: err.Error()}}
	}
	results := []services.CheckResult{{Name: "composer install", OK: true}}

	cache, err := NewCache()
	if err != nil {
		return append(results, services.CheckResult{Name: "drush site-install --existing-config", OK: false, Detail: err.Error()})
	}
	installer := drupal.NewInstaller(logger, drush.NewCLI(logger, cache), composerCLI)

	for _, site := range cfg.Sites {
		name := fmt.Sprintf("site %q installs from configuration", site)
		if err := installer.Install(ctx, path, site); err != nil {
			results = append(results, services.CheckResult{Name: name, OK: false, Detail: err.Error()})
			continue
		}
		results = append(results, services.CheckResult{Name: name, OK: true})
	}
	return results
}

// cleanupFullCheckArtifacts removes the clone and the SQLite databases/private files the site
// installs wrote beside it, mirroring the real run's own clone-mode cleanup.
func cleanupFullCheckArtifacts(path string, sites []string) {
	defer os.RemoveAll(path)

	parent := filepath.Dir(path)
	for _, site := range sites {
		os.Remove(filepath.Join(parent, site+".sqlite"))
		os.RemoveAll(filepath.Join(parent, "private", site))
	}
	os.Remove(filepath.Join(parent, "private"))
}

func printCheckResults(w io.Writer, results []services.CheckResult) {
	for _, r := range results {
		mark := "✓"
		if !r.OK {
			mark = "✗"
		}
		if !r.OK && r.Detail != "" {
			fmt.Fprintf(w, "%s %s: %s\n", mark, r.Name, r.Detail)
			continue
		}
		fmt.Fprintf(w, "%s %s\n", mark, r.Name)
	}
}

func anyCheckFailed(results []services.CheckResult) bool {
	for _, r := range results {
		if !r.OK {
			return true
		}
	}
	return false
}

func init() {
	checkCmd.Flags().BoolVar(&checkFull, "full", false,
		"Additionally clone the repository and verify each site installs from its exported "+
			"configuration (drush site-install --existing-config). Expensive: most of a real run's cost.")
	rootCmd.AddCommand(checkCmd)
}
