package cmd

import (
	"bytes"
	"errors"
	"testing"

	"github.com/drupdater/drupdater/internal"
	"github.com/drupdater/drupdater/internal/services"
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
		logger := NewLogger(config)

		// Assert logger is in debug mode
		assert.NotNil(t, logger)
		assert.True(t, logger.Core().Enabled(zapcore.DebugLevel))
	})

	t.Run("non-verbose mode", func(t *testing.T) {
		// Setup config with verbose mode disabled
		config := internal.Config{
			Verbose: false,
		}

		// Create logger
		logger := NewLogger(config)

		// Assert logger is not in debug mode but is in info mode
		assert.NotNil(t, logger)
		assert.False(t, logger.Core().Enabled(zapcore.DebugLevel))
		assert.True(t, logger.Core().Enabled(zapcore.InfoLevel))
	})
}

func TestNewCache(t *testing.T) {
	// Create cache
	cache := NewCache()

	// Verify cache is initialized
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

		abortErr := services.AbortError{Msg: "branch already exists, skipping"}
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
		abortErr := services.AbortError{Msg: "no changes detected"}
		require.NoError(t, errors.Unwrap(abortErr), "AbortError has no wrapped error, so Unwrap returns nil")
		assert.Equal(t, "no changes detected", abortErr.Error())
	})
}

func TestCreateDispatcher(t *testing.T) {
	logger := zaptest.NewLogger(t)

	t.Run("returns a non-nil dispatcher with addons subscribed", func(t *testing.T) {
		config := internal.Config{Addons: internal.AddonsConfig{Normal: []string{"composer_normalizer"}}}
		addons, err := createAddons(logger, config, nil, nil, nil, nil)
		require.NoError(t, err)
		dispatcher := createDispatcher(addons)
		assert.NotNil(t, dispatcher)
	})

	t.Run("works with an empty addon list", func(t *testing.T) {
		dispatcher := createDispatcher(nil)
		assert.NotNil(t, dispatcher)
	})
}

func TestCreateAddons(t *testing.T) {
	logger := zaptest.NewLogger(t)

	t.Run("mandatory addons plus the normal list", func(t *testing.T) {
		config := internal.Config{
			Addons: internal.AddonsConfig{Normal: []string{"code_beautifier"}},
		}
		addons, err := createAddons(logger, config, nil, nil, nil, nil)
		require.NoError(t, err)
		// 4 mandatory + code_beautifier
		assert.Len(t, addons, 5)
	})

	t.Run("security mode adds composer_audit even when not listed", func(t *testing.T) {
		config := internal.Config{
			Security: true,
			Addons: internal.AddonsConfig{
				Normal:   []string{"code_beautifier"},
				Security: []string{"code_beautifier"}, // composer_audit intentionally omitted
			},
		}
		addons, err := createAddons(logger, config, nil, nil, nil, nil)
		require.NoError(t, err)
		// 4 mandatory + composer_audit + code_beautifier
		assert.Len(t, addons, 6)
	})

	t.Run("a mandatory addon listed in the YAML is not duplicated", func(t *testing.T) {
		config := internal.Config{Addons: internal.AddonsConfig{Normal: []string{"update_hooks"}}}
		addons, err := createAddons(logger, config, nil, nil, nil, nil)
		require.NoError(t, err)
		assert.Len(t, addons, 4) // update_hooks is already mandatory
	})

	t.Run("an unknown addon name is an error", func(t *testing.T) {
		config := internal.Config{Addons: internal.AddonsConfig{Normal: []string{"does_not_exist"}}}
		_, err := createAddons(logger, config, nil, nil, nil, nil)
		require.Error(t, err)
	})
}

func TestValidateAddons(t *testing.T) {
	t.Run("known names pass, including mandatory ones", func(t *testing.T) {
		config := internal.Config{Addons: internal.AddonsConfig{
			Normal:   []string{"code_beautifier", "update_hooks"},
			Security: []string{"composer_normalizer"},
		}}
		require.NoError(t, validateAddons(config))
	})

	t.Run("unknown name in the normal list is an error", func(t *testing.T) {
		config := internal.Config{Addons: internal.AddonsConfig{Normal: []string{"nope"}}}
		err := validateAddons(config)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "nope")
	})

	t.Run("unknown name in the security list is caught regardless of mode", func(t *testing.T) {
		// Security: false, yet a bad security-list name must still be rejected.
		config := internal.Config{Addons: internal.AddonsConfig{Security: []string{"typo"}}}
		require.Error(t, validateAddons(config))
	})
}

func TestConfigurableAddons(t *testing.T) {
	names := configurableAddons()

	// Exactly the four configurable addons, sorted, and nothing mandatory.
	assert.Equal(t, []string{
		"code_beautifier",
		"composer_normalizer",
		"deprecations_remover",
		"translations_updater",
	}, names)

	for _, mandatory := range append(mandatoryAddons, "composer_audit") {
		assert.NotContains(t, names, mandatory)
	}
}

func TestAddonsCommand(t *testing.T) {
	var buf bytes.Buffer
	addonsCmd.SetOut(&buf)
	addonsCmd.Run(addonsCmd, nil)
	out := buf.String()

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
}
