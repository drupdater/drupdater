package cmd

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/drupdater/drupdater/internal"
	"github.com/drupdater/drupdater/internal/logging"
	"github.com/drupdater/drupdater/pkg/repo"
	git "github.com/go-git/go-git/v5"
	gitConfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
	"go.uber.org/zap/zaptest/observer"
)

func TestResolveToken(t *testing.T) {
	t.Run("the positional argument wins", func(t *testing.T) {
		t.Setenv("DRUPDATER_TOKEN", "from-env")

		token, err := resolveToken([]string{"from-arg"})
		require.NoError(t, err)
		assert.Equal(t, "from-arg", token)
	})

	t.Run("falls back to DRUPDATER_TOKEN", func(t *testing.T) {
		t.Setenv("DRUPDATER_TOKEN", "from-env")

		token, err := resolveToken(nil)
		require.NoError(t, err)
		assert.Equal(t, "from-env", token)
	})

	t.Run("an empty argument falls back to the environment", func(t *testing.T) {
		t.Setenv("DRUPDATER_TOKEN", "from-env")

		token, err := resolveToken([]string{""})
		require.NoError(t, err)
		assert.Equal(t, "from-env", token)
	})

	t.Run("errors when neither is set", func(t *testing.T) {
		t.Setenv("DRUPDATER_TOKEN", "")

		_, err := resolveToken(nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "DRUPDATER_TOKEN")
	})
}

func TestRegisterComposerAuth(t *testing.T) {
	// registerComposerAuth must register the individual credentials inside COMPOSER_AUTH, not
	// just the raw JSON blob: Composer echoes the username/password/token itself (typically
	// embedded in a URL after a failed authenticated fetch), never the blob verbatim.
	redact := func(t *testing.T, redactor *logging.Redactor, msg string) string {
		t.Helper()
		core, logs := observer.New(zap.DebugLevel)
		logger := zap.New(logging.WrapCore(redactor)(core))
		logger.Info(msg)
		return logs.All()[0].Message
	}

	t.Run("registers individual leaf values from valid JSON", func(t *testing.T) {
		redactor := logging.NewRedactor()
		registerComposerAuth(redactor, `{"http-basic":{"repo.packagist.com":{"username":"du","password":"s3cr3t-pass"}}}`)

		got := redact(t, redactor, "fetch failed: https://du:s3cr3t-pass@repo.packagist.com/p2/foo.json: 403")
		assert.NotContains(t, got, "s3cr3t-pass")
	})

	t.Run("falls back to redacting the raw value when it is not valid JSON", func(t *testing.T) {
		redactor := logging.NewRedactor()
		registerComposerAuth(redactor, "not-json-token")

		got := redact(t, redactor, "leaked not-json-token here")
		assert.NotContains(t, got, "not-json-token")
	})

	t.Run("empty value is a no-op", func(t *testing.T) {
		redactor := logging.NewRedactor()
		registerComposerAuth(redactor, "")

		got := redact(t, redactor, "hello world")
		assert.Equal(t, "hello world", got)
	})
}

func TestJSONStringLeaves(t *testing.T) {
	leaves := jsonStringLeaves(map[string]any{
		"a": "1",
		"b": map[string]any{"c": "2", "d": []any{"3", "4"}},
		"e": 5.0,
	})
	assert.ElementsMatch(t, []string{"1", "2", "3", "4"}, leaves)
}

func TestConfigFilePath(t *testing.T) {
	assert.Equal(t, "/explicit/config.yaml", configFilePath("/explicit/config.yaml", "/work"))
	assert.Equal(t, filepath.Join("/work", ".drupdater.yaml"), configFilePath("", "/work"))
}

func TestLoadProjectConfig(t *testing.T) {
	logger := zaptest.NewLogger(t)

	t.Run("a missing file applies the defaults", func(t *testing.T) {
		cfg := internal.Config{}
		require.NoError(t, loadProjectConfig(logger, filepath.Join(t.TempDir(), ".drupdater.yaml"), &cfg))

		assert.Equal(t, []string{"default"}, cfg.Sites)
		assert.Equal(t, 30*time.Minute, cfg.Timeout)
	})

	t.Run("a file's values are applied", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), ".drupdater.yaml")
		require.NoError(t, os.WriteFile(path, []byte("sites: [default, sub]\ntimeout: 90s\n"), 0o600))

		cfg := internal.Config{}
		require.NoError(t, loadProjectConfig(logger, path, &cfg))
		assert.Equal(t, []string{"default", "sub"}, cfg.Sites)
		assert.Equal(t, 90*time.Second, cfg.Timeout)
	})

	t.Run("an invalid file is an error", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), ".drupdater.yaml")
		require.NoError(t, os.WriteFile(path, []byte("timeout: nope\n"), 0o600))

		cfg := internal.Config{}
		require.Error(t, loadProjectConfig(logger, path, &cfg))
	})

	t.Run("an unknown addon name is an error", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), ".drupdater.yaml")
		require.NoError(t, os.WriteFile(path, []byte("addons:\n  normal: [no_such_addon]\n"), 0o600))

		cfg := internal.Config{}
		err := loadProjectConfig(logger, path, &cfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no_such_addon")
	})
}

// initCheckout creates a repo with one commit and an origin remote.
func initCheckout(t *testing.T, originURL string) string {
	t.Helper()
	dir := t.TempDir()
	r, err := git.PlainInit(dir, false)
	require.NoError(t, err)
	wt, err := r.Worktree()
	require.NoError(t, err)
	_, err = wt.Commit("init", &git.CommitOptions{
		AllowEmptyCommits: true,
		Author:            &object.Signature{Name: "t", Email: "t@example.com"},
	})
	require.NoError(t, err)
	if originURL != "" {
		_, err = r.CreateRemote(&gitConfig.RemoteConfig{Name: "origin", URLs: []string{originURL}})
		require.NoError(t, err)
	}
	return dir
}

func TestResolveCheckoutSettings(t *testing.T) {
	svc := repo.NewGitRepositoryService(zap.NewNop())

	t.Run("takes the URL and branch from the checkout", func(t *testing.T) {
		dir := initCheckout(t, "https://token:secret@example.com/group/repo.git")

		cfg := internal.Config{WorkingDir: dir, Branch: "main"}
		require.NoError(t, resolveCheckoutSettings(svc, &cfg))

		// Credentials embedded by CI are stripped.
		assert.Equal(t, "https://example.com/group/repo.git", cfg.RepositoryURL)
		// --branch is overridden by what the checkout actually has.
		assert.Equal(t, "master", cfg.Branch)
	})

	t.Run("keeps an explicitly given repository URL", func(t *testing.T) {
		dir := initCheckout(t, "https://example.com/group/repo.git")

		cfg := internal.Config{WorkingDir: dir, RepositoryURL: "https://example.com/other/repo.git"}
		require.NoError(t, resolveCheckoutSettings(svc, &cfg))
		assert.Equal(t, "https://example.com/other/repo.git", cfg.RepositoryURL)
	})

	t.Run("errors when there is no origin remote", func(t *testing.T) {
		cfg := internal.Config{WorkingDir: initCheckout(t, "")}
		err := resolveCheckoutSettings(svc, &cfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to determine repository URL from checkout")
	})

	t.Run("errors when the branch cannot be determined", func(t *testing.T) {
		t.Setenv("GITHUB_REF_NAME", "")
		t.Setenv("CI_COMMIT_REF_NAME", "")

		dir := initCheckout(t, "https://example.com/group/repo.git")
		r, err := git.PlainOpen(dir)
		require.NoError(t, err)
		head, err := r.Head()
		require.NoError(t, err)
		wt, err := r.Worktree()
		require.NoError(t, err)
		require.NoError(t, wt.Checkout(&git.CheckoutOptions{Hash: head.Hash()}))

		cfg := internal.Config{WorkingDir: dir}
		require.Error(t, resolveCheckoutSettings(svc, &cfg))
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

		ensureGitSafeDirectory(context.Background(), zap.NewNop(), dir)
		first := readEntries(t)
		assert.Contains(t, first, dir)

		// A second run must not append the same entry again.
		ensureGitSafeDirectory(context.Background(), zap.NewNop(), dir)
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

		ensureGitSafeDirectory(context.Background(), zap.NewNop(), dir)
		assert.NotContains(t, readEntries(t), dir)
	})

	t.Run("an unresolvable path is reported, not fatal", func(t *testing.T) {
		core, logs := observer.New(zap.WarnLevel)

		// A cancelled context makes the git invocation fail; the run must continue regardless,
		// because safe.directory is a convenience, not a requirement.
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		ensureGitSafeDirectory(ctx, zap.New(core), t.TempDir())
		assert.Positive(t, logs.Len())
	})
}

func TestRootCommandPreRunE(t *testing.T) {
	reset := func() {
		config = internal.Config{}
	}
	t.Cleanup(reset)

	t.Run("--clone without a repository URL is rejected", func(t *testing.T) {
		reset()
		config.Clone = true

		err := rootCmd.PreRunE(rootCmd, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "--repository-url is required")
	})

	t.Run("an SCP-style repository URL is accepted", func(t *testing.T) {
		reset()
		config.RepositoryURL = "git@github.com:drupdater/drupdater.git"

		require.NoError(t, rootCmd.PreRunE(rootCmd, nil))
	})

	t.Run("an HTTPS repository URL is accepted", func(t *testing.T) {
		reset()
		config.RepositoryURL = "https://github.com/drupdater/drupdater.git"

		require.NoError(t, rootCmd.PreRunE(rootCmd, nil))
	})

	t.Run("a malformed repository URL is rejected", func(t *testing.T) {
		reset()
		config.RepositoryURL = "not a url"

		err := rootCmd.PreRunE(rootCmd, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid repository URL")
	})

	t.Run("no repository URL is fine in checkout mode", func(t *testing.T) {
		reset()
		require.NoError(t, rootCmd.PreRunE(rootCmd, nil))
	})
}
