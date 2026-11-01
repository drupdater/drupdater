// Package scripts embeds the PHP drupdater runs inside a project: the drush commands loaded
// with --include.
package scripts

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"
)

//go:embed drush/Commands/*.php
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

// DrushInclude is the directory to pass to `drush --include`.
func DrushInclude(dir string) string {
	return filepath.Join(dir, "drush")
}
