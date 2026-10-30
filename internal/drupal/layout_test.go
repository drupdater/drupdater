package drupal

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
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

type fixedConfig struct {
	value string
	err   error
}

func (f fixedConfig) GetConfig(context.Context, string, string) (string, error) {
	return f.value, f.err
}

func TestWebRootDropsTheTrailingSlash(t *testing.T) {
	webroot, err := WebRoot(t.Context(), fixedConfig{value: "web/"}, "/project")
	require.NoError(t, err)
	assert.Equal(t, "web", webroot)
}

func TestWebRootPassesTheConfigErrorOn(t *testing.T) {
	_, err := WebRoot(t.Context(), fixedConfig{err: assert.AnError}, "/project")
	require.ErrorIs(t, err, assert.AnError)
}

func TestCustomCodeDirectoriesListsOnlyTheOnesThatExist(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/project/web/modules/custom", 0o755))
	require.NoError(t, fs.MkdirAll("/project/web/profiles/custom", 0o755))

	dirs, err := CustomCodeDirectories(t.Context(), fs, fixedConfig{value: "web"}, "/project")
	require.NoError(t, err)
	assert.Equal(t, []string{"web/modules/custom", "web/profiles/custom"}, dirs)
}

func TestCustomCodeDirectoriesIsEmptyWithoutAny(t *testing.T) {
	dirs, err := CustomCodeDirectories(t.Context(), afero.NewMemMapFs(), fixedConfig{value: "web"}, "/project")
	require.NoError(t, err)
	assert.Empty(t, dirs)
}

func TestCustomCodeDirectoriesPassesTheConfigErrorOn(t *testing.T) {
	_, err := CustomCodeDirectories(t.Context(), afero.NewMemMapFs(), fixedConfig{err: assert.AnError}, "/project")
	require.ErrorIs(t, err, assert.AnError)
}
