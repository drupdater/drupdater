package addon

import (
	"errors"

	"github.com/drupdater/drupdater/internal/workflow"
	"github.com/drupdater/drupdater/pkg/drush"
	"go.uber.org/zap"
)

// UnsupportedModules detects modules Drupal's update status service reports as NOT_SUPPORTED. The
// merge request lists them together with composer_audit's abandoned packages.
type UnsupportedModules struct {
	logger *zap.Logger

	modules workflow.PerSite[[]drush.UnsupportedModule]
}

// NewUnsupportedModules creates an unsupported modules detector.
func NewUnsupportedModules(logger *zap.Logger) *UnsupportedModules {
	return &UnsupportedModules{
		logger: logger,
	}
}

func (um *UnsupportedModules) Hooks() workflow.Hooks {
	return workflow.Hooks{
		PreSiteUpdate: workflow.On(workflow.Inspect, um.preSiteUpdateHandler),
	}
}

// preSiteUpdateHandler checks a site. Best-effort: an unreachable status service is not an error.
func (um *UnsupportedModules) preSiteUpdateHandler(evt *workflow.PreSiteUpdateEvent) error {

	facts := evt.Facts()
	if facts.UnsupportedModulesError != "" {
		um.logger.Warn("failed to check for unsupported modules", zap.String("site", evt.Site()), zap.Error(errors.New(facts.UnsupportedModulesError)))
		return nil
	}
	modules := facts.UnsupportedModules
	if len(modules) == 0 {
		return nil
	}

	um.modules.Set(evt.Site(), modules)

	um.logger.Info("unsupported modules found", zap.String("site", evt.Site()), zap.Int("count", len(modules)))

	return nil
}
