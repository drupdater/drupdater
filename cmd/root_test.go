package cmd

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
	"go.uber.org/zap/zaptest/observer"

	"github.com/drupdater/drupdater/internal/config"
	"github.com/drupdater/drupdater/internal/logging"
	"github.com/drupdater/drupdater/internal/repo"

	"bytes"
	"errors"

	"go.uber.org/zap/zapcore"

	"github.com/drupdater/drupdater/internal/workflow"
)

func TestNewLogger(t *testing.T) {
	t.Run("verbose mode", func(t *testing.T) {
		// Setup config with verbose mode enabled
		config := config.Config{
			Verbose: true,
		}

		// Create logger
		logger, err := NewLogger(config, logging.NewRedactor())

		// Assert logger is in debug mode
		require.NoError(t, err)
		assert.NotNil(t, logger)
		assert.True(t, logger.Core().Enabled(zapcore.DebugLevel))
	})

	t.Run("non-verbose mode", func(t *testing.T) {
		// Setup config with verbose mode disabled
		config := config.Config{
			Verbose: false,
		}

		// Create logger
		logger, err := NewLogger(config, logging.NewRedactor())

		// Assert logger is not in debug mode but is in info mode
		require.NoError(t, err)
		assert.NotNil(t, logger)
		assert.False(t, logger.Core().Enabled(zapcore.DebugLevel))
		assert.True(t, logger.Core().Enabled(zapcore.InfoLevel))
	})
}

func TestPersistentFlagDefaults(t *testing.T) {
	// Safety-critical: --security defaulting to true makes every run security-only, --clone
	// makes drupdater clone instead of updating the checkout it was pointed at.
	tests := []struct {
		flag string
		want string
	}{
		{flag: "branch", want: "main"},
		{flag: "working-dir", want: "."},
		{flag: "clone", want: "false"},
		{flag: "repository-url", want: ""},
		{flag: "security", want: "false"},
		{flag: "dry-run", want: "false"},
		{flag: "verbose", want: "false"},
		{flag: "config", want: ""},
		{flag: "report", want: ""},
	}

	flags := testApp().newRootCmd().PersistentFlags()
	for _, tt := range tests {
		t.Run(tt.flag, func(t *testing.T) {
			f := flags.Lookup(tt.flag)
			require.NotNil(t, f, "flag %q is not registered", tt.flag)
			assert.Equal(t, tt.want, f.DefValue)
			assert.NotEmpty(t, f.Usage, "every flag needs help text")
		})
	}
}

func TestHandleWorkflowError(t *testing.T) {
	t.Run("AbortError logs warning with message not nil", func(t *testing.T) {
		core, logs := observer.New(zap.WarnLevel)
		logger := zap.New(core)

		abortErr := workflow.AbortError{Msg: "branch already exists, skipping"}
		result := handleWorkflowError(logger, abortErr)

		require.NoError(t, result)
		assert.Equal(t, 1, logs.Len())
		assert.Equal(t, zap.WarnLevel, logs.All()[0].Level)
		assert.Equal(t, "update aborted", logs.All()[0].Message)
		assert.Equal(t, abortErr.Error(), logs.All()[0].ContextMap()["error"])
	})

	t.Run("regular error logs at error level and is returned", func(t *testing.T) {
		core, logs := observer.New(zap.ErrorLevel)
		logger := zap.New(core)

		regularErr := errors.New("something went wrong")
		result := handleWorkflowError(logger, regularErr)

		require.ErrorIs(t, result, regularErr)
		assert.Equal(t, 1, logs.Len())
		assert.Equal(t, zap.ErrorLevel, logs.All()[0].Level)
	})

	t.Run("errors.Unwrap returns nil for AbortError confirming fix is needed", func(t *testing.T) {
		abortErr := workflow.AbortError{Msg: "no changes detected"}
		require.NoError(t, errors.Unwrap(abortErr), "AbortError has no wrapped error, so Unwrap returns nil")
		assert.Equal(t, "no changes detected", abortErr.Error())
	})
}

func TestValidateAddons(t *testing.T) {
	t.Run("known names pass, including mandatory ones", func(t *testing.T) {
		config := config.Config{RunTypes: config.RunTypesConfig{
			Normal:   config.RunTypeConfig{Addons: []string{"code_beautifier", "update_hooks"}},
			Security: config.RunTypeConfig{Addons: []string{"composer_normalizer"}},
		}}
		require.NoError(t, agentCatalog.validate(config))
	})

	t.Run("unknown name in the normal list is an error", func(t *testing.T) {
		config := config.Config{RunTypes: config.RunTypesConfig{Normal: config.RunTypeConfig{Addons: []string{"nope"}}}}
		err := agentCatalog.validate(config)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "nope")
	})

	t.Run("unknown name in the security list is caught regardless of mode", func(t *testing.T) {
		// Security: false, yet a bad security-list name must still be rejected.
		config := config.Config{RunTypes: config.RunTypesConfig{Security: config.RunTypeConfig{Addons: []string{"typo"}}}}
		require.Error(t, agentCatalog.validate(config))
	})
}

func TestConfigurableAddons(t *testing.T) {
	names := agentCatalog.configurable()

	// Exactly the four configurable addons, sorted, and nothing mandatory.
	assert.Equal(t, []string{
		"code_beautifier",
		"composer_normalizer",
		"deprecations_remover",
		"translations_updater",
	}, names)

	for _, mandatory := range append(agentCatalog.mandatory, "composer_audit") {
		assert.NotContains(t, names, mandatory)
	}
}

func TestResolveCheckoutBranch(t *testing.T) {
	svc := repo.New()

	// initRepo creates a repo with one commit; detach leaves HEAD off any branch.
	initRepo := func(t *testing.T, detach bool) string {
		dir := t.TempDir()
		gitInitWithCommit(t, dir)
		if detach {
			gitIn(t, dir, "checkout", "--quiet", "--detach")
		}
		return dir
	}

	t.Run("uses the checkout's current branch", func(t *testing.T) {
		branch, err := resolveCheckoutBranch(t.Context(), svc, initRepo(t, false))
		require.NoError(t, err)
		assert.Equal(t, "main", branch)
	})

	t.Run("falls back to CI variable when detached", func(t *testing.T) {
		t.Setenv("GITHUB_REF_NAME", "release-1")
		branch, err := resolveCheckoutBranch(t.Context(), svc, initRepo(t, true))
		require.NoError(t, err)
		assert.Equal(t, "release-1", branch)
	})

	t.Run("errors when detached and no CI variable", func(t *testing.T) {
		t.Setenv("GITHUB_REF_NAME", "")
		t.Setenv("CI_COMMIT_REF_NAME", "")
		_, err := resolveCheckoutBranch(t.Context(), svc, initRepo(t, true))
		require.Error(t, err)
	})

	t.Run("falls back to the GitLab CI variable", func(t *testing.T) {
		// Both CI variables: covering only GitHub would let the GitLab operand be dropped.
		t.Setenv("GITHUB_REF_NAME", "")
		t.Setenv("CI_COMMIT_REF_NAME", "release-2")
		branch, err := resolveCheckoutBranch(t.Context(), svc, initRepo(t, true))
		require.NoError(t, err)
		assert.Equal(t, "release-2", branch)
	})

	t.Run("prefers the GitHub variable when both are set", func(t *testing.T) {
		t.Setenv("GITHUB_REF_NAME", "from-github")
		t.Setenv("CI_COMMIT_REF_NAME", "from-gitlab")
		branch, err := resolveCheckoutBranch(t.Context(), svc, initRepo(t, true))
		require.NoError(t, err)
		assert.Equal(t, "from-github", branch)
	})

	t.Run("errors when the path is not a checkout", func(t *testing.T) {
		_, err := resolveCheckoutBranch(t.Context(), svc, t.TempDir())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to determine branch from checkout")
		require.ErrorIs(t, err, repo.ErrNotARepository, "the cause must survive the wrapper")
	})
}

func TestComposerOptions(t *testing.T) {
	assert.Len(t, composerOptions(config.Config{Security: true}), 1, "a security run turns blocking off")
	assert.Empty(t, composerOptions(config.Config{}), "a normal run keeps the project's policy")
}

func TestCommandTreesShareNoFlagState(t *testing.T) {
	first := testApp().newRootCmd()
	require.NoError(t, first.PersistentFlags().Parse([]string{"--branch", "develop", "--dry-run"}))

	second := testApp().newRootCmd()

	assert.Equal(t, "main", second.PersistentFlags().Lookup("branch").Value.String())
	assert.Equal(t, "false", second.PersistentFlags().Lookup("dry-run").Value.String())
}

func TestRootCommandValidatesTheFlagsItParsed(t *testing.T) {
	cmd := testApp().newRootCmd()
	cmd.SetArgs([]string{"--clone"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	require.ErrorContains(t, cmd.ExecuteContext(t.Context()), "--repository-url is required")
}

func TestCheckCommandReadsTheRootFlags(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir)
	reportPath := filepath.Join(t.TempDir(), "check.json")

	root := testApp().newRootCmd()
	root.SetArgs([]string{"check", "--working-dir", dir, "--report", reportPath})
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})

	require.ErrorContains(t, root.ExecuteContext(t.Context()), "preflight check failed")
	assert.FileExists(t, reportPath, "--report, a root flag, has to reach the check command")
}

func TestCheckCommandHasTheFullFlag(t *testing.T) {
	check, _, err := testApp().newRootCmd().Find([]string{"check"})
	require.NoError(t, err)

	full := check.Flags().Lookup("full")
	require.NotNil(t, full)
	assert.Equal(t, "false", full.DefValue)
}

func TestConfigFilePath(t *testing.T) {
	assert.Equal(t, "/explicit/config.yaml", configFilePath("/explicit/config.yaml", "/work"))
	assert.Equal(t, filepath.Join("/work", ".drupdater.yaml"), configFilePath("", "/work"))
}

func TestLoadProjectConfig(t *testing.T) {
	logger := zaptest.NewLogger(t)

	t.Run("a missing file applies the defaults", func(t *testing.T) {
		cfg := config.Config{}
		require.NoError(t, loadProjectConfig(logger, agentCatalog, filepath.Join(t.TempDir(), ".drupdater.yaml"), &cfg))

		assert.Equal(t, []string{"default"}, cfg.Sites)
		assert.Equal(t, 30*time.Minute, cfg.Timeout)
	})

	t.Run("a file's values are applied", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), ".drupdater.yaml")
		require.NoError(t, os.WriteFile(path, []byte("sites: [default, sub]\ntimeout: 90s\n"), 0o600))

		cfg := config.Config{}
		require.NoError(t, loadProjectConfig(logger, agentCatalog, path, &cfg))
		assert.Equal(t, []string{"default", "sub"}, cfg.Sites)
		assert.Equal(t, 90*time.Second, cfg.Timeout)
	})

	t.Run("an invalid file is an error", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), ".drupdater.yaml")
		require.NoError(t, os.WriteFile(path, []byte("timeout: nope\n"), 0o600))

		cfg := config.Config{}
		require.Error(t, loadProjectConfig(logger, agentCatalog, path, &cfg))
	})

	t.Run("an unknown addon name is an error", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), ".drupdater.yaml")
		require.NoError(t, os.WriteFile(path, []byte("run_types:\n  normal:\n    addons: [no_such_addon]\n"), 0o600))

		cfg := config.Config{}
		err := loadProjectConfig(logger, agentCatalog, path, &cfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no_such_addon")
	})
}

func TestValidateFlags(t *testing.T) {
	t.Run("--clone without a repository URL is rejected", func(t *testing.T) {
		err := validateFlags(config.Config{Clone: true})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "--repository-url is required")
	})

	t.Run("an SCP-style repository URL is accepted", func(t *testing.T) {
		require.NoError(t, validateFlags(config.Config{RepositoryURL: "git@github.com:drupdater/drupdater.git"}))
	})

	t.Run("an HTTPS repository URL is accepted", func(t *testing.T) {
		require.NoError(t, validateFlags(config.Config{RepositoryURL: "https://github.com/drupdater/drupdater.git"}))
	})

	t.Run("a malformed repository URL is rejected", func(t *testing.T) {
		err := validateFlags(config.Config{RepositoryURL: "not a url"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid repository URL")
	})

	t.Run("no repository URL is fine in checkout mode", func(t *testing.T) {
		require.NoError(t, validateFlags(config.Config{}))
	})
}
