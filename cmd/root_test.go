package cmd

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"testing"

	"github.com/drupdater/drupdater/internal"
	"github.com/drupdater/drupdater/internal/addon"
	"github.com/drupdater/drupdater/internal/logging"
	"github.com/drupdater/drupdater/internal/workflow"
	"github.com/drupdater/drupdater/pkg/composer"
	"github.com/drupdater/drupdater/pkg/repo"
	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"
	"go.uber.org/zap/zaptest/observer"
)

func TestNewLogger(t *testing.T) {
	t.Run("verbose mode", func(t *testing.T) {
		// Setup config with verbose mode enabled
		config := internal.Config{
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
		config := internal.Config{
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

	// Not a literal: it reflects the container's CPU quota, so pin the source rather than a
	// number that differs per machine.
	concurrency := flags.Lookup("concurrency")
	require.NotNil(t, concurrency)
	assert.Equal(t, strconv.Itoa(runtime.GOMAXPROCS(0)), concurrency.DefValue)
}

func TestNewCache(t *testing.T) {
	// Create cache
	cache, err := NewCache()

	// Verify cache is initialized
	require.NoError(t, err)
	assert.NotNil(t, cache)

	// Test basic cache operations
	cache.Set("test_key", "test_value")
	value, found := cache.Get("test_key")

	assert.True(t, found)
	assert.Equal(t, "test_value", value)
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

func TestCreateAddons(t *testing.T) {
	logger := zaptest.NewLogger(t)

	t.Run("mandatory addons plus the normal list", func(t *testing.T) {
		config := internal.Config{
			RunTypes: internal.RunTypesConfig{Normal: internal.RunTypeConfig{Addons: []string{"code_beautifier"}}},
		}
		addons, err := builtinAddons().create(logger, config, nil, nil, nil)
		require.NoError(t, err)
		assert.Len(t, addons, len(builtinAddons().mandatory)+1)
	})

	t.Run("composer_audit and unsupported_modules run on a normal update", func(t *testing.T) {
		// Both are mandatory: the description joins them into one list, so either alone shows half of it.
		config := internal.Config{RunTypes: internal.RunTypesConfig{Normal: internal.RunTypeConfig{}}}
		addons, err := builtinAddons().create(logger, config, nil, nil, nil)
		require.NoError(t, err)
		assert.Len(t, addons, len(builtinAddons().mandatory))
		assert.Contains(t, builtinAddons().mandatory, "composer_audit")
		assert.Contains(t, builtinAddons().mandatory, "unsupported_modules")
	})

	// composer_audit runs in both modes, so --security must reach it some other way. Pinned on
	// the one difference visible here: only a security run aborts when nothing is vulnerable.
	for name, tt := range map[string]struct {
		security bool
		aborts   bool
	}{
		"security mode lets composer_audit abort a run with no advisories": {security: true, aborts: true},
		"a normal run is not narrowed to advisories":                       {security: false, aborts: false},
	} {
		t.Run(name, func(t *testing.T) {
			config := internal.Config{Security: tt.security}
			addons, err := builtinAddons().create(logger, config, nil, cleanAudit{}, nil)
			require.NoError(t, err)

			audit := addons[slices.Index(builtinAddons().mandatory, "composer_audit")]
			_, err = audit.Hooks().PreComposerUpdate.Run(workflow.NewPreComposerUpdateEvent(t.Context(), "/repo", nil, workflow.UpdatePlan{}))

			_, aborted := errors.AsType[workflow.AbortError](err)
			assert.Equal(t, tt.aborts, aborted, "error: %v", err)
		})
	}

	t.Run("a mandatory addon listed in the YAML is not duplicated", func(t *testing.T) {
		config := internal.Config{RunTypes: internal.RunTypesConfig{Normal: internal.RunTypeConfig{Addons: []string{"update_hooks"}}}}
		addons, err := builtinAddons().create(logger, config, nil, nil, nil)
		require.NoError(t, err)
		assert.Len(t, addons, len(builtinAddons().mandatory)) // update_hooks is already mandatory
	})

	t.Run("an unknown addon name is an error", func(t *testing.T) {
		config := internal.Config{RunTypes: internal.RunTypesConfig{Normal: internal.RunTypeConfig{Addons: []string{"does_not_exist"}}}}
		_, err := builtinAddons().create(logger, config, nil, nil, nil)
		require.Error(t, err)
	})
}

func TestValidateAddons(t *testing.T) {
	t.Run("known names pass, including mandatory ones", func(t *testing.T) {
		config := internal.Config{RunTypes: internal.RunTypesConfig{
			Normal:   internal.RunTypeConfig{Addons: []string{"code_beautifier", "update_hooks"}},
			Security: internal.RunTypeConfig{Addons: []string{"composer_normalizer"}},
		}}
		require.NoError(t, builtinAddons().validate(config))
	})

	t.Run("unknown name in the normal list is an error", func(t *testing.T) {
		config := internal.Config{RunTypes: internal.RunTypesConfig{Normal: internal.RunTypeConfig{Addons: []string{"nope"}}}}
		err := builtinAddons().validate(config)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "nope")
	})

	t.Run("unknown name in the security list is caught regardless of mode", func(t *testing.T) {
		// Security: false, yet a bad security-list name must still be rejected.
		config := internal.Config{RunTypes: internal.RunTypesConfig{Security: internal.RunTypeConfig{Addons: []string{"typo"}}}}
		require.Error(t, builtinAddons().validate(config))
	})
}

func TestConfigurableAddons(t *testing.T) {
	names := builtinAddons().configurable()

	// Exactly the four configurable addons, sorted, and nothing mandatory.
	assert.Equal(t, []string{
		"code_beautifier",
		"composer_normalizer",
		"deprecations_remover",
		"translations_updater",
	}, names)

	for _, mandatory := range append(builtinAddons().mandatory, "composer_audit") {
		assert.NotContains(t, names, mandatory)
	}
}

func TestAddonsCommand(t *testing.T) {
	var buf bytes.Buffer
	addons := testApp().newAddonsCmd()
	addons.SetOut(&buf)
	addons.Run(addons, nil)
	out := buf.String()

	assert.Contains(t, out, "Addons you can set under run_types.normal.addons")
	assert.Contains(t, out, "code_beautifier")
	// Mandatory addons must not be listed as settable.
	assert.NotContains(t, out, "composer_patches")
	assert.NotContains(t, out, "composer_audit")
}

func TestResolveCheckoutBranch(t *testing.T) {
	svc := repo.NewGitRepositoryService(zap.NewNop())

	// initRepo creates a repo with one commit; detach leaves HEAD off any branch.
	initRepo := func(t *testing.T, detach bool) string {
		dir := t.TempDir()
		r, err := git.PlainInit(dir, false)
		require.NoError(t, err)
		wt, err := r.Worktree()
		require.NoError(t, err)
		h, err := wt.Commit("init", &git.CommitOptions{
			AllowEmptyCommits: true,
			Author:            &object.Signature{Name: "t", Email: "t@example.com"},
		})
		require.NoError(t, err)
		if detach {
			require.NoError(t, wt.Checkout(&git.CheckoutOptions{Hash: h}))
		}
		return dir
	}

	t.Run("uses the checkout's current branch", func(t *testing.T) {
		branch, err := resolveCheckoutBranch(svc, initRepo(t, false))
		require.NoError(t, err)
		assert.Equal(t, "master", branch)
	})

	t.Run("falls back to CI variable when detached", func(t *testing.T) {
		t.Setenv("GITHUB_REF_NAME", "release-1")
		branch, err := resolveCheckoutBranch(svc, initRepo(t, true))
		require.NoError(t, err)
		assert.Equal(t, "release-1", branch)
	})

	t.Run("errors when detached and no CI variable", func(t *testing.T) {
		t.Setenv("GITHUB_REF_NAME", "")
		t.Setenv("CI_COMMIT_REF_NAME", "")
		_, err := resolveCheckoutBranch(svc, initRepo(t, true))
		require.Error(t, err)
	})

	t.Run("falls back to the GitLab CI variable", func(t *testing.T) {
		// Both CI variables: covering only GitHub would let the GitLab operand be dropped.
		t.Setenv("GITHUB_REF_NAME", "")
		t.Setenv("CI_COMMIT_REF_NAME", "release-2")
		branch, err := resolveCheckoutBranch(svc, initRepo(t, true))
		require.NoError(t, err)
		assert.Equal(t, "release-2", branch)
	})

	t.Run("prefers the GitHub variable when both are set", func(t *testing.T) {
		t.Setenv("GITHUB_REF_NAME", "from-github")
		t.Setenv("CI_COMMIT_REF_NAME", "from-gitlab")
		branch, err := resolveCheckoutBranch(svc, initRepo(t, true))
		require.NoError(t, err)
		assert.Equal(t, "from-github", branch)
	})

	t.Run("errors when the path is not a checkout", func(t *testing.T) {
		_, err := resolveCheckoutBranch(svc, t.TempDir())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to determine branch from checkout")
		require.ErrorIs(t, err, git.ErrRepositoryNotExists, "the cause must survive the wrapper")
	})
}

func TestComposerOptions(t *testing.T) {
	assert.Len(t, composerOptions(internal.Config{Security: true}), 1, "a security run turns blocking off")
	assert.Empty(t, composerOptions(internal.Config{}), "a normal run keeps the project's policy")
}

// cleanAudit is a composer whose audit finds nothing. Only Audit is implemented.
type cleanAudit struct{ addon.Composer }

func (cleanAudit) Audit(context.Context, string) (composer.Audit, error) {
	return composer.Audit{}, nil
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
	_, err := git.PlainInit(dir, false)
	require.NoError(t, err)
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
