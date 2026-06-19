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
		require.NoError(t, LoadConfigFile(filepath.Join(t.TempDir(), "absent.yaml"), &c))

		assert.Equal(t, []string{"default"}, c.Sites)
		assert.Equal(t, 30*time.Minute, c.Timeout)
		assert.Equal(t, defaultRegularAddons, c.Addons.Regular)
		assert.Equal(t, defaultSecurityAddons, c.Addons.Security)
	})

	t.Run("partial file keeps defaults for absent keys", func(t *testing.T) {
		var c Config
		require.NoError(t, LoadConfigFile(writeConfig(t, "addons:\n  regular: [code_beautifier]\n"), &c))

		assert.Equal(t, []string{"code_beautifier"}, c.Addons.Regular) // overridden
		assert.Equal(t, defaultSecurityAddons, c.Addons.Security)      // default kept
		assert.Equal(t, []string{"default"}, c.Sites)                  // default kept
		assert.Equal(t, 30*time.Minute, c.Timeout)                     // default kept
	})

	t.Run("timeout string parses into a duration", func(t *testing.T) {
		var c Config
		require.NoError(t, LoadConfigFile(writeConfig(t, "timeout: 90s\n"), &c))
		assert.Equal(t, 90*time.Second, c.Timeout)
	})

	t.Run("invalid timeout is an error", func(t *testing.T) {
		var c Config
		require.Error(t, LoadConfigFile(writeConfig(t, "timeout: not-a-duration\n"), &c))
	})
}
