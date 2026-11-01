package composer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/drupdater/drupdater/internal/agent"
)

// Project is what the agent's drupdater:project reports: composer.json as written, and the lock
// as Composer reads it. A snapshot: ask again once composer.json or the lock may have changed.
type Project struct {
	// WebRoot is extra.drupal-scaffold.locations.web-root, without a trailing slash; "" when unset.
	WebRoot string `json:"web_root"`
	// CustomCodeDirectories are the web root's modules/custom, themes/custom and profiles/custom
	// that exist, relative to the project.
	CustomCodeDirectories []string `json:"custom_code_directories"`
	// LockHash is composer.lock's content-hash; "" without a lock.
	LockHash string `json:"lock_hash"`
	// Packages are the locked packages, dev ones included, by name.
	Packages map[string]LockedPackage `json:"packages"`
	// DependencyPatches maps a package to the patch files locked dependencies declare for it,
	// which composer-patches 1.x applies on top of the root's.
	DependencyPatches map[string][]string `json:"dependency_patches"`
	// AllowPlugins is config.allow-plugins as written; nil when composer.json has none.
	AllowPlugins json.RawMessage `json:"allow_plugins"`
	// Patches is extra.patches as written; nil when composer.json has none.
	Patches json.RawMessage `json:"patches"`
}

type LockedPackage struct {
	Version string `json:"version"`
	// Plugin is true for a package Composer checks against allow-plugins.
	Plugin bool `json:"plugin"`
}

var (
	errNoWebRoot = errors.New("extra.drupal-scaffold.locations.web-root is not defined")
	errNoLock    = errors.New("composer.lock has no content-hash")
)

// Project asks the agent about the project in dir.
func (s *CLI) Project(ctx context.Context, dir string) (Project, error) {
	project, err := agent.Call[Project](ctx, s.command(dir), "drupdater:project", struct{}{})
	if err != nil {
		return Project{}, fmt.Errorf("could not read the project: %w", err)
	}
	return project, nil
}

// RequireWebRoot is WebRoot, or an error where a Drupal project must have one.
func (p Project) RequireWebRoot() (string, error) {
	if p.WebRoot == "" {
		return "", errNoWebRoot
	}
	return p.WebRoot, nil
}

// CustomCode is CustomCodeDirectories, or an error without a web root to find them under.
func (p Project) CustomCode() ([]string, error) {
	if p.WebRoot == "" {
		return nil, errNoWebRoot
	}
	return p.CustomCodeDirectories, nil
}

func (p Project) RequireLockHash() (string, error) {
	if p.LockHash == "" {
		return "", errNoLock
	}
	return p.LockHash, nil
}

// Installed reports whether the lock has name, as a dependency or a dev dependency.
func (p Project) Installed(name string) bool {
	_, ok := p.Packages[name]
	return ok
}

// Version is name's locked version, "" when it is not locked.
func (p Project) Version(name string) string {
	return p.Packages[name].Version
}

// Plugins are the locked plugins' names, sorted.
func (p Project) Plugins() []string {
	var plugins []string
	for _, name := range slices.Sorted(maps.Keys(p.Packages)) {
		if p.Packages[name].Plugin {
			plugins = append(plugins, name)
		}
	}
	return plugins
}

// ParseAllowPlugins reads Project.AllowPlugins. It keeps "no setting" apart from an empty one,
// which must be restored differently.
func ParseAllowPlugins(raw json.RawMessage) (AllowPlugins, error) {
	trimmed := strings.TrimSpace(string(raw))
	switch trimmed {
	case "", "null":
		return AllowPlugins{}, nil
	case "true", "false":
		all := trimmed == "true"
		return AllowPlugins{Set: true, All: &all}, nil
	}

	rules := map[string]bool{}
	if err := json.Unmarshal(raw, &rules); err != nil {
		return AllowPlugins{}, fmt.Errorf("failed to parse composer allow-plugins config %s: %w", trimmed, err)
	}
	return AllowPlugins{Set: true, Rules: rules}, nil
}
