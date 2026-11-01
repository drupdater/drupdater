package cmd

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/drupdater/drupdater/internal/config"
	"github.com/drupdater/drupdater/internal/logging"
	"github.com/drupdater/drupdater/internal/workflow"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestCheckToken(t *testing.T) {
	t.Run("the positional argument wins", func(t *testing.T) {
		t.Setenv("DRUPDATER_TOKEN", "from-env")
		assert.Equal(t, "from-arg", checkToken([]string{"from-arg"}))
	})

	t.Run("falls back to DRUPDATER_TOKEN", func(t *testing.T) {
		t.Setenv("DRUPDATER_TOKEN", "from-env")
		assert.Equal(t, "from-env", checkToken(nil))
	})

	t.Run("no token at all is not an error, just empty", func(t *testing.T) {
		t.Setenv("DRUPDATER_TOKEN", "")
		assert.Empty(t, checkToken(nil))
	})
}

func TestCheckConfigAndAddons(t *testing.T) {
	t.Run("a missing file applies the defaults and passes", func(t *testing.T) {
		cfg := config.Config{}
		results := checkConfigAndAddons(builtinAddons(), filepath.Join(t.TempDir(), ".drupdater.yaml"), &cfg)

		require.Len(t, results, 2)
		assert.True(t, results[0].OK)
		assert.Contains(t, results[0].Name, "sites: default")
		assert.True(t, results[1].OK)
		assert.Equal(t, "addon names resolve", results[1].Name)
	})

	t.Run("an invalid file stops at the first check", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), ".drupdater.yaml")
		require.NoError(t, os.WriteFile(path, []byte("timeout: nope\n"), 0o600))

		cfg := config.Config{}
		results := checkConfigAndAddons(builtinAddons(), path, &cfg)

		require.Len(t, results, 1)
		assert.False(t, results[0].OK)
		assert.Equal(t, ".drupdater.yaml valid", results[0].Name)
	})

	t.Run("an unknown addon name fails the second check only", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), ".drupdater.yaml")
		require.NoError(t, os.WriteFile(path, []byte("run_types:\n  normal:\n    addons: [no_such_addon]\n"), 0o600))

		cfg := config.Config{}
		results := checkConfigAndAddons(builtinAddons(), path, &cfg)

		require.Len(t, results, 2)
		assert.True(t, results[0].OK)
		assert.False(t, results[1].OK)
		assert.Contains(t, results[1].Detail, "no_such_addon")
	})
}

func TestCheckVCS(t *testing.T) {
	ctx := t.Context()
	logger := zap.NewNop()

	// Every branch returns the same check name. It is what the user reads in the output, so an
	// unnamed result would be useless even when OK and Detail are right.
	const hostCheck = "repository host recognized (GitHub/GitLab)"

	t.Run("no repository URL and no resolve error", func(t *testing.T) {
		results := testApp().checkVCS(ctx, logger, "", "", nil)
		require.Len(t, results, 1)
		assert.Equal(t, hostCheck, results[0].Name)
		assert.False(t, results[0].OK)
		assert.Contains(t, results[0].Detail, "could not determine repository URL")
	})

	t.Run("no repository URL surfaces the resolve error", func(t *testing.T) {
		results := testApp().checkVCS(ctx, logger, "", "", errors.New("no origin remote"))
		require.Len(t, results, 1)
		assert.Equal(t, hostCheck, results[0].Name)
		assert.False(t, results[0].OK)
		assert.Equal(t, "no origin remote", results[0].Detail)
	})

	t.Run("an unrecognized host fails", func(t *testing.T) {
		results := testApp().checkVCS(ctx, logger, "not a url", "", nil)
		require.Len(t, results, 1)
		assert.Equal(t, hostCheck, results[0].Name)
		assert.False(t, results[0].OK)
		assert.NotEmpty(t, results[0].Detail, "a failure has to say why")
	})

	t.Run("a recognized host with no token stops after the host check", func(t *testing.T) {
		results := testApp().checkVCS(ctx, logger, "https://github.com/acme/site.git", "", nil)
		require.Len(t, results, 1)
		assert.Equal(t, hostCheck, results[0].Name)
		assert.True(t, results[0].OK)
		assert.Empty(t, results[0].Detail, "a passing check needs no detail")
	})
}

func TestPrintCheckResults(t *testing.T) {
	var buf bytes.Buffer
	printCheckResults(&buf, []workflow.CheckResult{
		{Name: "a", OK: true},
		{Name: "b", OK: false, Detail: "went wrong"},
		{Name: "c", OK: false},
	}, logging.NewRedactor())

	out := buf.String()
	assert.Contains(t, out, "✓ a\n")
	assert.Contains(t, out, "✗ b: went wrong\n")
	assert.Contains(t, out, "✗ c\n")
}

func TestPrintCheckResultsRedactsDetail(t *testing.T) {
	// Detail can carry raw subprocess output, where a credential would reach stdout unredacted.
	redactor := logging.NewRedactor()
	redactor.Register("s3cr3t-token")

	var buf bytes.Buffer
	printCheckResults(&buf, []workflow.CheckResult{
		{Name: "composer install", OK: false, Detail: "fetch failed: https://s3cr3t-token@example.com/repo.git: 403"},
	}, redactor)

	out := buf.String()
	assert.NotContains(t, out, "s3cr3t-token")
	assert.Contains(t, out, "***")
}

func TestAnyCheckFailed(t *testing.T) {
	assert.False(t, anyCheckFailed([]workflow.CheckResult{{OK: true}, {OK: true}}))
	assert.True(t, anyCheckFailed([]workflow.CheckResult{{OK: true}, {OK: false}}))
	assert.False(t, anyCheckFailed(nil))
}

// Doubles for the narrow interfaces runCheapChecks needs, so its branching runs without the
// composer/drush/git binaries CI doesn't install.
type fakeShallowCloneChecker struct {
	shallow bool
	err     error
}

func (f fakeShallowCloneChecker) IsShallowClone(context.Context, string) (bool, error) {
	return f.shallow, f.err
}

type fakeCheapChecksComposer struct {
	platformReqsOut string
	platformReqsErr error
	webRoot         string
	webRootErr      error
	agentErr        error
}

func (f fakeCheapChecksComposer) CheckAgent(context.Context, string) error {
	return f.agentErr
}

func (f fakeCheapChecksComposer) CheckPlatformReqs(context.Context, string) (string, error) {
	return f.platformReqsOut, f.platformReqsErr
}

func (f fakeCheapChecksComposer) GetConfig(context.Context, string, string) (string, error) {
	return f.webRoot, f.webRootErr
}

func TestRunCheapChecks(t *testing.T) {
	ctx := t.Context()
	logger := zap.NewNop()

	newFS := func(t *testing.T, workingDir string, sites ...string) afero.Fs {
		t.Helper()
		fs := afero.NewMemMapFs()
		for _, site := range sites {
			require.NoError(t, afero.WriteFile(fs, filepath.Join(workingDir, "web/sites", site, "settings.php"), []byte("<?php"), 0o644))
		}
		return fs
	}

	t.Run("everything passes", func(t *testing.T) {
		cfg := &config.Config{WorkingDir: "/project", RepositoryURL: "https://github.com/acme/site.git"}
		repository := fakeShallowCloneChecker{shallow: false}
		composerSvc := fakeCheapChecksComposer{webRoot: "web"}

		results := testApp().runCheapChecks(ctx, logger, filepath.Join(t.TempDir(), ".drupdater.yaml"), cfg, repository, composerSvc, newFS(t, "/project", "default"), "", nil)

		require.False(t, anyCheckFailed(results))
		// config valid, addons resolve, git history, platform reqs, agent, site settings, VCS host.
		assert.Len(t, results, 7)
	})

	t.Run("a shallow clone and unmet platform reqs both surface as failures", func(t *testing.T) {
		cfg := &config.Config{WorkingDir: "/project", RepositoryURL: "https://github.com/acme/site.git"}
		repository := fakeShallowCloneChecker{shallow: true}
		composerSvc := fakeCheapChecksComposer{platformReqsOut: "php 8.1 required", platformReqsErr: errors.New("unmet")}

		results := testApp().runCheapChecks(ctx, logger, filepath.Join(t.TempDir(), ".drupdater.yaml"), cfg, repository, composerSvc, newFS(t, "/project", "default"), "", nil)

		require.True(t, anyCheckFailed(results))
		var names []string
		for _, r := range results {
			if !r.OK {
				names = append(names, r.Name)
			}
		}
		assert.Contains(t, names, "git history complete (not a shallow clone)")
		assert.Contains(t, names, "PHP platform requirements satisfied")
	})

	t.Run("a missing agent surfaces as a failure", func(t *testing.T) {
		cfg := &config.Config{WorkingDir: "/project", RepositoryURL: "https://github.com/acme/site.git"}
		composerSvc := fakeCheapChecksComposer{webRoot: "web", agentErr: errors.New("command drupdater:handshake not defined")}

		results := testApp().runCheapChecks(ctx, logger, filepath.Join(t.TempDir(), ".drupdater.yaml"), cfg, fakeShallowCloneChecker{}, composerSvc, newFS(t, "/project", "default"), "", nil)

		var failed []workflow.CheckResult
		for _, r := range results {
			if !r.OK {
				failed = append(failed, r)
			}
		}
		require.Len(t, failed, 1)
		assert.Equal(t, "drupdater agent installed in Composer", failed[0].Name)
		assert.Contains(t, failed[0].Detail, "not defined")
	})

	t.Run("one result per configured site", func(t *testing.T) {
		// The sites come from the config file, not a pre-set cfg.Sites. Only "default" has
		// a settings.php.
		cfgPath := filepath.Join(t.TempDir(), ".drupdater.yaml")
		require.NoError(t, os.WriteFile(cfgPath, []byte("sites: [default, extra]\n"), 0o600))

		cfg := &config.Config{WorkingDir: "/project", RepositoryURL: "https://github.com/acme/site.git"}
		repository := fakeShallowCloneChecker{}
		composerSvc := fakeCheapChecksComposer{webRoot: "web"}

		results := testApp().runCheapChecks(ctx, logger, cfgPath, cfg, repository, composerSvc, newFS(t, "/project", "default"), "", nil)

		var siteFailures int
		for _, r := range results {
			if !r.OK && r.Name == `site "extra": settings.php` {
				siteFailures++
			}
		}
		assert.Equal(t, 1, siteFailures)
	})
}

func TestRunFullChecksNoRepositoryURL(t *testing.T) {
	results := testApp().runFullChecks(t.Context(), zap.NewNop(), config.Config{}, "")
	require.Len(t, results, 1)
	assert.Equal(t, "sites install from configuration", results[0].Name)
	assert.False(t, results[0].OK)
	assert.Contains(t, results[0].Detail, "no repository URL to clone")
}

func TestRunFullChecksDefaultsToMainBranch(t *testing.T) {
	// With no branch configured the clone must still try one. It fails here, but the detail
	// names the branch it tried.
	cfg := config.Config{RepositoryURL: t.TempDir()}

	results := testApp().runFullChecks(t.Context(), zap.NewNop(), cfg, "")
	require.Len(t, results, 1)
	assert.Equal(t, "clone for full check", results[0].Name)
	assert.False(t, results[0].OK)
	assert.NotEmpty(t, results[0].Detail)
}

func TestRunFullChecksCloneFailure(t *testing.T) {
	// A non-repository path as the "repository URL" fails fast in CloneRepository itself, so
	// this exercises the clone-error branch without needing composer or drush at all.
	cfg := config.Config{RepositoryURL: t.TempDir(), Branch: "main"}

	results := testApp().runFullChecks(t.Context(), zap.NewNop(), cfg, "")
	require.Len(t, results, 1)
	assert.False(t, results[0].OK)
	assert.Equal(t, "clone for full check", results[0].Name)
}
