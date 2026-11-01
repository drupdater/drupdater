package drupal

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// PHP's realpath() resolves symlinks, so the project directory has to be resolved before the
// prefix can match — on macOS every temporary directory sits behind the /tmp symlink.
func TestRelativeToResolvesTheProjectDirectory(t *testing.T) {
	resolved, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	link := filepath.Join(t.TempDir(), "project")
	require.NoError(t, os.Symlink(resolved, link))

	assert.Equal(t, filepath.Join("config", "sync"), RelativeTo(link, filepath.Join(resolved, "config", "sync")))
	assert.Equal(t, filepath.Join("config", "sync"), RelativeTo(resolved, filepath.Join(resolved, "config", "sync")))
}

func TestRelativeToTakesAnUnresolvedPathUnderASymlinkedDirectory(t *testing.T) {
	link := filepath.Join(t.TempDir(), "project")
	require.NoError(t, os.Symlink(t.TempDir(), link))

	assert.Equal(t, filepath.Join("config", "sync"), RelativeTo(link, filepath.Join(link, "config", "sync")))
}

func TestRelativeToLeavesAPathOutsideTheDirectoryAbsolute(t *testing.T) {
	assert.Equal(t, "/elsewhere/config", RelativeTo("/project", "/elsewhere/config"))
	assert.Equal(t, "/", RelativeTo("/project", "/"))
}

func TestRelativeToFallsBack(t *testing.T) {
	assert.Equal(t, "config/sync", RelativeTo("/does/not/exist", "/does/not/exist/config/sync"),
		"an unresolvable directory is compared as given")
	assert.Empty(t, RelativeTo("/project", ""), "realpath() of a missing directory stays empty")
	assert.Equal(t, "config/sync", RelativeTo("/project", "config/sync"), "a relative path is already relative")
}
