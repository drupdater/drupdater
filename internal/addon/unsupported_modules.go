package addon

import (
	"errors"

	"github.com/drupdater/drupdater/internal/drush"
	"github.com/drupdater/drupdater/internal/workflow"
	"go.uber.org/zap"
)

// UnsupportedModules detects modules Drupal's update status service reports as NOT_SUPPORTED. The
// merge request lists them together with composer_audit's abandoned packages.
type UnsupportedModules struct {
	logger *zap.Logger
}

// NewUnsupportedModules creates an unsupported modules detector.
func NewUnsupportedModules(logger *zap.Logger) *UnsupportedModules {
	return &UnsupportedModules{
		logger: logger,
	}
}

func (um *UnsupportedModules) Hooks() workflow.Hooks {
	return workflow.Hooks{
		PreSiteUpdate: workflow.Observe(um.preSiteUpdateHandler),
	}
}

// preSiteUpdateHandler checks a site. Best-effort: an unreachable status service is not an error.
func (um *UnsupportedModules) preSiteUpdateHandler(evt *workflow.PreSiteUpdateEvent) ([]drush.UnsupportedModule, error) {
	facts := evt.Facts()
	if facts.UnsupportedModulesError != "" {
		um.logger.Warn("failed to check for unsupported modules", zap.String("site", evt.Site()), zap.Error(errors.New(facts.UnsupportedModulesError)))
		return nil, nil
	}
	modules := facts.UnsupportedModules
	if len(modules) > 0 {
		um.logger.Info("unsupported modules found", zap.String("site", evt.Site()), zap.Int("count", len(modules)))
	}
	return modules, nil
}
