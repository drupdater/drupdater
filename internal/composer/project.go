package composer

import (
	"context"
	"errors"
	"fmt"

	"github.com/drupdater/drupdater/internal/agent"
)

// Project is what the agent's drupdater:project reports about the project. A snapshot: ask again
// once composer.json or the lock may have changed.
type Project struct {
	// WebRoot is extra.drupal-scaffold.locations.web-root, without a trailing slash; "" when unset.
	WebRoot string `json:"web_root"`
	// LockHash is composer.lock's content-hash; "" without a lock.
	LockHash string `json:"lock_hash"`
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

func (p Project) RequireLockHash() (string, error) {
	if p.LockHash == "" {
		return "", errNoLock
	}
	return p.LockHash, nil
}
