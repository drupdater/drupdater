// Package scripts embeds the PHP drupdater runs inside a project: Rector's config and the
// drush commands loaded with --include.
package scripts

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"
)

//go:embed *.php drush/Commands/*.php
var files embed.FS

// Extract writes the embedded files into a new temporary directory, which the caller removes.
func Extract() (string, error) {
	return extract(files)
}

func extract(fsys fs.FS) (string, error) {
	dir, err := os.MkdirTemp("", "drupdater-scripts-")
	if err != nil {
		return "", err
	}
	if err := os.CopyFS(dir, fsys); err != nil {
		os.RemoveAll(dir)
		return "", err
	}
	return dir, nil
}

// RectorConfig is the Rector config inside a directory Extract wrote.
func RectorConfig(dir string) string {
	return filepath.Join(dir, "rector.php")
}

// DrushInclude is the directory to pass to `drush --include`.
func DrushInclude(dir string) string {
	return filepath.Join(dir, "drush")
}
