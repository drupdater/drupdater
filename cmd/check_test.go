package cmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/drupdater/drupdater/internal"
	"github.com/drupdater/drupdater/internal/services"
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
		cfg := internal.Config{}
		results := checkConfigAndAddons(filepath.Join(t.TempDir(), ".drupdater.yaml"), &cfg)

		require.Len(t, results, 2)
		assert.True(t, results[0].OK)
		assert.Contains(t, results[0].Name, "sites: default")
		assert.True(t, results[1].OK)
		assert.Equal(t, "addon names resolve", results[1].Name)
	})

	t.Run("an invalid file stops at the first check", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), ".drupdater.yaml")
		require.NoError(t, os.WriteFile(path, []byte("timeout: nope\n"), 0o600))

		cfg := internal.Config{}
		results := checkConfigAndAddons(path, &cfg)

		require.Len(t, results, 1)
		assert.False(t, results[0].OK)
		assert.Equal(t, ".drupdater.yaml valid", results[0].Name)
	})

	t.Run("an unknown addon name fails the second check only", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), ".drupdater.yaml")
		require.NoError(t, os.WriteFile(path, []byte("addons:\n  normal: [no_such_addon]\n"), 0o600))

		cfg := internal.Config{}
		results := checkConfigAndAddons(path, &cfg)

		require.Len(t, results, 2)
		assert.True(t, results[0].OK)
		assert.False(t, results[1].OK)
		assert.Contains(t, results[1].Detail, "no_such_addon")
	})
}

func TestCheckVCS(t *testing.T) {
	ctx := t.Context()
	logger := zap.NewNop()

	t.Run("no repository URL and no resolve error", func(t *testing.T) {
		results := checkVCS(ctx, logger, "", "", nil)
		require.Len(t, results, 1)
		assert.False(t, results[0].OK)
		assert.Contains(t, results[0].Detail, "could not determine repository URL")
	})

	t.Run("no repository URL surfaces the resolve error", func(t *testing.T) {
		results := checkVCS(ctx, logger, "", "", errors.New("no origin remote"))
		require.Len(t, results, 1)
		assert.False(t, results[0].OK)
		assert.Equal(t, "no origin remote", results[0].Detail)
	})

	t.Run("an unrecognized host fails", func(t *testing.T) {
		results := checkVCS(ctx, logger, "not a url", "", nil)
		require.Len(t, results, 1)
		assert.False(t, results[0].OK)
	})

	t.Run("a recognized host with no token stops after the host check", func(t *testing.T) {
		results := checkVCS(ctx, logger, "https://github.com/acme/site.git", "", nil)
		require.Len(t, results, 1)
		assert.True(t, results[0].OK)
	})
}

func TestPrintCheckResults(t *testing.T) {
	var buf bytes.Buffer
	printCheckResults(&buf, []services.CheckResult{
		{Name: "a", OK: true},
		{Name: "b", OK: false, Detail: "went wrong"},
		{Name: "c", OK: false},
	})

	out := buf.String()
	assert.Contains(t, out, "✓ a\n")
	assert.Contains(t, out, "✗ b: went wrong\n")
	assert.Contains(t, out, "✗ c\n")
}

func TestAnyCheckFailed(t *testing.T) {
	assert.False(t, anyCheckFailed([]services.CheckResult{{OK: true}, {OK: true}}))
	assert.True(t, anyCheckFailed([]services.CheckResult{{OK: true}, {OK: false}}))
	assert.False(t, anyCheckFailed(nil))
}

func TestCleanupFullCheckArtifacts(t *testing.T) {
	parent := t.TempDir()
	clone := filepath.Join(parent, "repo123")
	require.NoError(t, os.MkdirAll(clone, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(parent, "default.sqlite"), []byte("x"), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(parent, "private", "default"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(parent, "private", "default", "f.txt"), []byte("x"), 0o600))

	cleanupFullCheckArtifacts(clone, []string{"default"})

	assert.NoDirExists(t, clone)
	assert.NoFileExists(t, filepath.Join(parent, "default.sqlite"))
	assert.NoDirExists(t, filepath.Join(parent, "private", "default"))
	// The private parent is removed too, since nothing else claims it.
	assert.NoDirExists(t, filepath.Join(parent, "private"))
}

func TestCleanupFullCheckArtifactsKeepsUnrelatedPrivateData(t *testing.T) {
	parent := t.TempDir()
	clone := filepath.Join(parent, "repo123")
	require.NoError(t, os.MkdirAll(clone, 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(parent, "private", "default"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(parent, "private", "other-stuff"), 0o755))

	cleanupFullCheckArtifacts(clone, []string{"default"})

	assert.NoDirExists(t, filepath.Join(parent, "private", "default"))
	// A non-empty "private" dir (something this run didn't create) must survive.
	assert.DirExists(t, filepath.Join(parent, "private", "other-stuff"))
}
