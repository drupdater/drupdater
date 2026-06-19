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
	t.Run("missing file returns defaults", func(t *testing.T) {
		fc, err := LoadConfigFile(filepath.Join(t.TempDir(), "absent.yaml"))
		require.NoError(t, err)
		assert.Equal(t, defaultFileConfig(), fc)
	})

	t.Run("partial file keeps defaults for absent keys", func(t *testing.T) {
		fc, err := LoadConfigFile(writeConfig(t, "addons:\n  regular: [code_beautifier]\n"))
		require.NoError(t, err)

		assert.Equal(t, []string{"code_beautifier"}, fc.Addons.Regular) // overridden
		assert.Equal(t, defaultSecurityAddons, fc.Addons.Security)      // default kept
		assert.Equal(t, []string{"default"}, fc.Sites)                  // default kept
		assert.Equal(t, "30m", fc.Timeout)                              // default kept
	})

	t.Run("timeout string parses into a duration via Apply", func(t *testing.T) {
		fc, err := LoadConfigFile(writeConfig(t, "timeout: 90s\n"))
		require.NoError(t, err)

		var c Config
		fc.Apply(&c)
		assert.Equal(t, 90*time.Second, c.Timeout)
	})

	t.Run("invalid timeout is an error", func(t *testing.T) {
		_, err := LoadConfigFile(writeConfig(t, "timeout: not-a-duration\n"))
		require.Error(t, err)
	})
}
