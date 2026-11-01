// Package drupal knows where a Drupal project keeps things.
package drupal

import (
	"path/filepath"
	"strings"
)

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
