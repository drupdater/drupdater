package internal

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".drupdater.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

func TestLoadConfigFile(t *testing.T) {
	t.Run("missing file applies defaults", func(t *testing.T) {
		var c Config
		found, err := LoadConfigFile(filepath.Join(t.TempDir(), "absent.yaml"), &c)
		require.NoError(t, err)
		assert.False(t, found)

		assert.Equal(t, []string{"default"}, c.Sites)
		assert.Equal(t, 30*time.Minute, c.Timeout)
		assert.Equal(t, defaultNormalAddons, c.RunTypes.Normal.Addons)
		assert.Empty(t, c.RunTypes.Security.Addons) // security is minimal by default
	})

	t.Run("partial file keeps defaults for absent keys", func(t *testing.T) {
		var c Config
		found, err := LoadConfigFile(writeConfig(t, "run_types:\n  normal:\n    addons: [code_beautifier]\n"), &c)
		require.NoError(t, err)
		assert.True(t, found)

		assert.Equal(t, []string{"code_beautifier"}, c.RunTypes.Normal.Addons) // overridden
		assert.Empty(t, c.RunTypes.Security.Addons)                            // default kept (minimal)
		assert.Equal(t, []string{"default"}, c.Sites)                          // default kept
		assert.Equal(t, 30*time.Minute, c.Timeout)                             // default kept
	})

	t.Run("timeout string parses into a duration", func(t *testing.T) {
		var c Config
		_, err := LoadConfigFile(writeConfig(t, "timeout: 90s\n"), &c)
		require.NoError(t, err)
		assert.Equal(t, 90*time.Second, c.Timeout)
	})

	t.Run("timeout 0 disables the timeout", func(t *testing.T) {
		var c Config
		_, err := LoadConfigFile(writeConfig(t, "timeout: 0\n"), &c)
		require.NoError(t, err)
		assert.Equal(t, time.Duration(0), c.Timeout)
	})

	t.Run("invalid timeout is an error", func(t *testing.T) {
		var c Config
		_, err := LoadConfigFile(writeConfig(t, "timeout: not-a-duration\n"), &c)
		require.Error(t, err)
	})

	t.Run("unknown key is rejected", func(t *testing.T) {
		var c Config
		_, err := LoadConfigFile(writeConfig(t, "timout: 30m\n"), &c) // typo
		require.Error(t, err)
		assert.Contains(t, err.Error(), "timout")
	})

	t.Run("an empty file applies defaults", func(t *testing.T) {
		// An empty document decodes to io.EOF. That means "no keys set", the same as an absent
		// file — it must not fail the run.
		var c Config
		found, err := LoadConfigFile(writeConfig(t, ""), &c)
		require.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, []string{"default"}, c.Sites)
		assert.Equal(t, 30*time.Minute, c.Timeout)
		assert.Equal(t, defaultNormalAddons, c.RunTypes.Normal.Addons)
	})

	t.Run("a comments-only file applies defaults", func(t *testing.T) {
		var c Config
		found, err := LoadConfigFile(writeConfig(t, "# everything is commented out\n# sites: [a]\n"), &c)
		require.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, []string{"default"}, c.Sites)
	})

	t.Run("an unreadable path is an error", func(t *testing.T) {
		var c Config
		// A directory is not os.IsNotExist, so it must surface rather than fall back.
		_, err := LoadConfigFile(t.TempDir(), &c)
		require.Error(t, err)
	})

	t.Run("an explicitly empty site list is rejected", func(t *testing.T) {
		// Every per-site phase iterates this list, so an empty one would skip installing,
		// updating and exporting for all sites while still opening a merge request.
		var c Config
		_, err := LoadConfigFile(writeConfig(t, "sites: []\n"), &c)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no sites configured")
	})

	t.Run("a null site list is rejected", func(t *testing.T) {
		var c Config
		_, err := LoadConfigFile(writeConfig(t, "sites:\n"), &c)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no sites configured")
	})

	t.Run("sites are applied when set", func(t *testing.T) {
		var c Config
		_, err := LoadConfigFile(writeConfig(t, "sites: [default, subsite_a]\n"), &c)
		require.NoError(t, err)
		assert.Equal(t, []string{"default", "subsite_a"}, c.Sites)
	})

	t.Run("a run type sets its addons and auto_merge together", func(t *testing.T) {
		var c Config
		_, err := LoadConfigFile(writeConfig(t, `run_types:
  normal:
    addons: [code_beautifier]
    auto_merge: false
  security:
    addons: []
    auto_merge: true
`), &c)
		require.NoError(t, err)
		assert.Equal(t, []string{"code_beautifier"}, c.RunTypes.Normal.Addons)
		assert.False(t, c.RunTypes.Normal.AutoMerge)
		assert.Empty(t, c.RunTypes.Security.Addons)
		assert.True(t, c.RunTypes.Security.AutoMerge)
	})

	t.Run("auto_merge defaults to false for both run types", func(t *testing.T) {
		var c Config
		found, err := LoadConfigFile(filepath.Join(t.TempDir(), "absent.yaml"), &c)
		require.NoError(t, err)
		assert.False(t, found)
		assert.False(t, c.RunTypes.Normal.AutoMerge)
		assert.False(t, c.RunTypes.Security.AutoMerge)
	})

	t.Run("ActiveRunType follows the security flag", func(t *testing.T) {
		c := Config{RunTypes: RunTypesConfig{
			Normal:   RunTypeConfig{Addons: []string{"code_beautifier"}, AutoMerge: false},
			Security: RunTypeConfig{Addons: []string{"composer_audit"}, AutoMerge: true},
		}}
		assert.Equal(t, []string{"code_beautifier"}, c.ActiveRunType().Addons)
		assert.False(t, c.ActiveRunType().AutoMerge)

		c.Security = true
		assert.Equal(t, []string{"composer_audit"}, c.ActiveRunType().Addons)
		assert.True(t, c.ActiveRunType().AutoMerge)
	})

	t.Run("the pre-run_types layout fails with a migration message", func(t *testing.T) {
		// Strict decoding alone would say "field addons not found in type internal.fileConfig",
		// which does not tell the reader what to write instead.
		for _, body := range []string{
			"addons:\n  normal: [code_beautifier]\n",
			"auto_merge:\n  normal: true\n",
		} {
			var c Config
			_, err := LoadConfigFile(writeConfig(t, body), &c)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "grouped per run type")
			assert.Contains(t, err.Error(), "run_types:")
		}
	})
}
