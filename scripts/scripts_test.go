package scripts

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractWritesTheFilesWhereTheirPathsPointDrushAndRector(t *testing.T) {
	dir, err := Extract()
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })

	assert.FileExists(t, RectorConfig(dir))
	assert.FileExists(t, filepath.Join(dir, "phpstan-drupal-bootstrap.php"), "rector.php includes it by __DIR__")
	// drush --include discovers Commands/*Commands.php under the directory it is given.
	assert.FileExists(t, filepath.Join(DrushInclude(dir), "Commands", "DrupdaterCommands.php"))
}

func TestExtractRemovesWhatItWroteWhenACopyFails(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	unreadable := fstest.MapFS{
		"rector.php": {Data: []byte("<?php\n")},
		"pipe.php":   {Mode: fs.ModeNamedPipe},
	}

	_, err := extract(unreadable)

	require.Error(t, err)
	matches, globErr := filepath.Glob(filepath.Join(os.TempDir(), "drupdater-scripts-*"))
	require.NoError(t, globErr)
	assert.Empty(t, matches)
}

func TestExtractFailsWithoutATemporaryDirectory(t *testing.T) {
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "missing"))

	_, err := Extract()
	require.Error(t, err)
}
