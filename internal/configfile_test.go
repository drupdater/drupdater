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
		assert.Equal(t, defaultNormalAddons, c.Addons.Normal)
		assert.Empty(t, c.Addons.Security) // security is minimal by default
	})

	t.Run("partial file keeps defaults for absent keys", func(t *testing.T) {
		var c Config
		found, err := LoadConfigFile(writeConfig(t, "addons:\n  normal: [code_beautifier]\n"), &c)
		require.NoError(t, err)
		assert.True(t, found)

		assert.Equal(t, []string{"code_beautifier"}, c.Addons.Normal) // overridden
		assert.Empty(t, c.Addons.Security)                            // default kept (minimal)
		assert.Equal(t, []string{"default"}, c.Sites)                 // default kept
		assert.Equal(t, 30*time.Minute, c.Timeout)                    // default kept
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

	t.Run("auto_merge per mode is read correctly", func(t *testing.T) {
		var c Config
		_, err := LoadConfigFile(writeConfig(t, "auto_merge:\n  normal: true\n  security: false\n"), &c)
		require.NoError(t, err)
		assert.True(t, c.AutoMerge.Normal)
		assert.False(t, c.AutoMerge.Security)
	})

	t.Run("auto_merge defaults to false for both modes", func(t *testing.T) {
		var c Config
		found, err := LoadConfigFile(filepath.Join(t.TempDir(), "absent.yaml"), &c)
		require.NoError(t, err)
		assert.False(t, found)
		assert.False(t, c.AutoMerge.Normal)
		assert.False(t, c.AutoMerge.Security)
	})
}
