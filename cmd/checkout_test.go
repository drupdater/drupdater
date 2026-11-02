package cmd

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/drupdater/drupdater/internal/config"
	"github.com/drupdater/drupdater/internal/repo"
)

// initCheckout creates a repo with one commit and an origin remote.
func initCheckout(t *testing.T, originURL string) string {
	t.Helper()
	dir := t.TempDir()
	gitInitWithCommit(t, dir)
	if originURL != "" {
		gitIn(t, dir, "remote", "add", "origin", originURL)
	}
	return dir
}

func TestResolveCheckoutSettings(t *testing.T) {
	svc := repo.New()

	t.Run("takes the URL and branch from the checkout", func(t *testing.T) {
		dir := initCheckout(t, "https://token:secret@example.com/group/repo.git")

		cfg := config.Config{WorkingDir: dir, Branch: "main"}
		require.NoError(t, resolveCheckoutSettings(t.Context(), svc, &cfg))

		// Credentials embedded by CI are stripped.
		assert.Equal(t, "https://example.com/group/repo.git", cfg.RepositoryURL)
		// --branch is overridden by what the checkout actually has.
		assert.Equal(t, "main", cfg.Branch)
	})

	t.Run("keeps an explicitly given repository URL", func(t *testing.T) {
		dir := initCheckout(t, "https://example.com/group/repo.git")

		cfg := config.Config{WorkingDir: dir, RepositoryURL: "https://example.com/other/repo.git"}
		require.NoError(t, resolveCheckoutSettings(t.Context(), svc, &cfg))
		assert.Equal(t, "https://example.com/other/repo.git", cfg.RepositoryURL)
	})

	t.Run("errors when there is no origin remote", func(t *testing.T) {
		cfg := config.Config{WorkingDir: initCheckout(t, "")}
		err := resolveCheckoutSettings(t.Context(), svc, &cfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to determine repository URL from checkout")
	})

	t.Run("errors when the branch cannot be determined", func(t *testing.T) {
		t.Setenv("GITHUB_REF_NAME", "")
		t.Setenv("CI_COMMIT_REF_NAME", "")

		dir := initCheckout(t, "https://example.com/group/repo.git")
		gitIn(t, dir, "checkout", "--quiet", "--detach")

		cfg := config.Config{WorkingDir: dir}
		require.Error(t, resolveCheckoutSettings(t.Context(), svc, &cfg))
	})
}

func TestEnsureGitSafeDirectory(t *testing.T) {
	// Point git's global config at a scratch file so the test never touches the real one.
	withScratchGitConfig := func(t *testing.T) string {
		t.Helper()
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
		return filepath.Join(home, ".gitconfig")
	}

	readEntries := func(t *testing.T) string {
		t.Helper()
		out, err := exec.CommandContext(t.Context(), "git", "config", "--global", "--get-all", "safe.directory").Output()
		if err != nil {
			return ""
		}
		return string(out)
	}

	t.Run("adds the checkout once and does not duplicate it", func(t *testing.T) {
		if _, err := exec.LookPath("git"); err != nil {
			t.Skip("git binary not available")
		}
		gitconfig := withScratchGitConfig(t)
		dir := t.TempDir()

		ensureGitSafeDirectory(t.Context(), zap.NewNop(), dir)
		first := readEntries(t)
		assert.Contains(t, first, dir)

		// A second run must not append the same entry again.
		ensureGitSafeDirectory(t.Context(), zap.NewNop(), dir)
		assert.Equal(t, first, readEntries(t))

		assert.FileExists(t, gitconfig)
	})

	t.Run("a wildcard entry already covers the checkout", func(t *testing.T) {
		if _, err := exec.LookPath("git"); err != nil {
			t.Skip("git binary not available")
		}
		withScratchGitConfig(t)
		dir := t.TempDir()

		require.NoError(t, exec.CommandContext(t.Context(), "git", "config", "--global", "--add", "safe.directory", "*").Run())

		ensureGitSafeDirectory(t.Context(), zap.NewNop(), dir)
		assert.NotContains(t, readEntries(t), dir)
	})

	t.Run("an unresolvable path is reported, not fatal", func(t *testing.T) {
		core, logs := observer.New(zap.WarnLevel)

		// A cancelled context makes the git invocation fail; the run must continue regardless,
		// because safe.directory is a convenience, not a requirement.
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		ensureGitSafeDirectory(ctx, zap.New(core), t.TempDir())
		assert.Positive(t, logs.Len())
	})
}
