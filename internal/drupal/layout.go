// Package drupal knows where a Drupal project keeps things.
package drupal

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/afero"
)

// ConfigGetter is the one method WebRoot needs, so a caller's own interface satisfies it.
type ConfigGetter interface {
	GetConfig(ctx context.Context, dir string, key string) (string, error)
}

// WebRoot returns the project's Drupal web root, relative to dir and without a trailing slash.
func WebRoot(ctx context.Context, cfg ConfigGetter, dir string) (string, error) {
	webroot, err := cfg.GetConfig(ctx, dir, "extra.drupal-scaffold.locations.web-root")
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(webroot, "/"), nil
}

// CustomCodeDirectories lists the custom module, theme and profile directories that exist, relative to dir.
func CustomCodeDirectories(ctx context.Context, fs afero.Fs, cfg ConfigGetter, dir string) ([]string, error) {
	webroot, err := WebRoot(ctx, cfg, dir)
	if err != nil {
		return nil, err
	}

	var found []string
	for _, kind := range []string{"modules", "themes", "profiles"} {
		candidate := webroot + "/" + kind + "/custom"
		if _, err := fs.Stat(dir + "/" + candidate); os.IsNotExist(err) {
			continue
		}
		found = append(found, candidate)
	}
	return found, nil
}

// RelativeTo makes an absolute path from drush relative to dir, which is tried as given and
// resolved: drush reports the resolved Drupal root, settings.php whatever it was written as.
func RelativeTo(dir string, path string) string {
	if !filepath.IsAbs(path) {
		return path
	}
	roots := []string{dir}
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		roots = append(roots, resolved)
	}
	for _, root := range roots {
		if rel, err := filepath.Rel(root, path); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return rel
		}
	}
	return path
}
