package cmd

import (
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

		assert.NoError(t, result)
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

		assert.ErrorIs(t, result, regularErr)
		assert.Equal(t, 1, logs.Len())
		assert.Equal(t, zap.ErrorLevel, logs.All()[0].Level)
	})

	t.Run("errors.Unwrap returns nil for AbortError confirming fix is needed", func(t *testing.T) {
		abortErr := services.AbortError{Msg: "no changes detected"}
		assert.Nil(t, errors.Unwrap(abortErr), "AbortError has no wrapped error, so Unwrap returns nil")
		assert.Equal(t, "no changes detected", abortErr.Error())
	})
}

func TestCreateDispatcher(t *testing.T) {
	logger := zaptest.NewLogger(t)

	t.Run("returns a non-nil dispatcher with addons subscribed", func(t *testing.T) {
		config := internal.Config{SkipCBF: true, SkipRector: true}
		addons := createAddons(logger, config, nil, nil, nil, nil)
		dispatcher := createDispatcher(addons)
		assert.NotNil(t, dispatcher)
	})

	t.Run("works with an empty addon list", func(t *testing.T) {
		dispatcher := createDispatcher(nil)
		assert.NotNil(t, dispatcher)
	})
}

func TestCreateAddons(t *testing.T) {
	// Setup minimal test dependencies
	logger := zaptest.NewLogger(t)

	// Test with default config
	t.Run("default config", func(t *testing.T) {
		config := internal.Config{
			SkipCBF:    true,  // Skip code beautifier
			SkipRector: true,  // Skip rector
			Security:   false, // No security-only updates
		}

		addons := createAddons(
			logger,
			config,
			nil, // mock would be better here
			nil, // mock would be better here
			nil, // mock would be better here
			nil, // mock would be better here
		)

		// Should have the basic addons (6 addons)
		assert.Len(t, addons, 6)
	})

	// Test with security mode enabled
	t.Run("security mode", func(t *testing.T) {
		config := internal.Config{
			SkipCBF:    true,
			SkipRector: true,
			Security:   true, // Enable security-only updates
		}

		addons := createAddons(
			logger,
			config,
			nil, // mock would be better here
			nil, // mock would be better here
			nil, // mock would be better here
			nil, // mock would be better here
		)

		// Should have the basic addons + security addon
		assert.Len(t, addons, 7)
	})
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
