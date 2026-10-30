// Package scripts embeds the PHP drupdater runs inside a project: Rector's config and the
// drush commands loaded with --include.
package scripts

import (
	"embed"
	"os"
	"path/filepath"
)

//go:embed *.php drush/Commands/*.php
var files embed.FS

// Extract writes the embedded files into dir, keeping their layout.
func Extract(dir string) error {
	return os.CopyFS(dir, files)
}

// RectorConfig is the Rector config inside a directory Extract wrote.
func RectorConfig(dir string) string {
	return filepath.Join(dir, "rector.php")
}

// DrushInclude is the directory to pass to `drush --include`.
func DrushInclude(dir string) string {
	return filepath.Join(dir, "drush")
}
