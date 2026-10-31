// Package external is the contract between drupdater and an addon built outside it. The public
// sdk package re-exports it; cmd reads it, so the two never import each other.
package external

import (
	"context"
	"fmt"
	"regexp"

	"github.com/drupdater/drupdater/internal/workflow"
	"go.uber.org/zap"
)

// Composer runs composer under the environment drupdater's own calls use.
type Composer interface {
	Exec(ctx context.Context, dir string, args ...string) (string, error)
}

// Drush runs drush for one site of the project, through the project's own composer exec.
type Drush interface {
	Exec(ctx context.Context, dir string, site string, args ...string) (string, error)
}

// Deps is what an external addon is built with, once per run.
type Deps struct {
	Logger   *zap.Logger
	Composer Composer
	Drush    Drush
	// Security marks a --security run.
	Security bool
}

// Registration names an addon for .drupdater.yaml and builds it.
type Registration struct {
	// Name is what a project lists under run_types.<type>.addons, and the addon's report key.
	Name string
	New  func(Deps) workflow.Addon
}

var namePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// Validate checks what can be checked before a run: the name's shape and that New is set.
func (r Registration) Validate() error {
	if !namePattern.MatchString(r.Name) {
		return fmt.Errorf("addon name %q must be lowercase letters, digits and underscores, starting with a letter", r.Name)
	}
	if r.New == nil {
		return fmt.Errorf("addon %q has no constructor", r.Name)
	}
	return nil
}
