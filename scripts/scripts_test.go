package scripts

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractWritesTheFilesWhereTheirPathsPointDrushAndRector(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, Extract(dir))

	assert.FileExists(t, RectorConfig(dir))
	assert.FileExists(t, filepath.Join(dir, "phpstan-drupal-bootstrap.php"), "rector.php includes it by __DIR__")
	// drush --include discovers Commands/*Commands.php under the directory it is given.
	assert.FileExists(t, filepath.Join(DrushInclude(dir), "Commands", "DrupdaterCommands.php"))
}

func TestExtractFailsIntoAnUnwritableDirectory(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))

	require.Error(t, Extract(file))
}
