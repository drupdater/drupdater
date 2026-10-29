package addon

import (
	"sync"

	"github.com/drupdater/drupdater/internal/workflow"
	"github.com/drupdater/drupdater/pkg/drush"
	"go.uber.org/zap"
)

// UnsupportedModules detects modules Drupal's update status service reports as NOT_SUPPORTED. The
// merge request lists them together with composer_audit's abandoned packages.
type UnsupportedModules struct {
	logger *zap.Logger
	drush  Drush

	// mu guards modules: preSiteUpdateHandler runs concurrently for each site. Keyed by name,
	// so multisite results are deduplicated.
	mu      sync.Mutex
	modules map[string]drush.UnsupportedModule
}

// NewUnsupportedModules creates an unsupported modules detector.
func NewUnsupportedModules(logger *zap.Logger, drushClient Drush) *UnsupportedModules {
	return &UnsupportedModules{
		logger:  logger,
		drush:   drushClient,
		modules: make(map[string]drush.UnsupportedModule),
	}
}

func (um *UnsupportedModules) Hooks() workflow.Hooks {
	return workflow.Hooks{
		PreSiteUpdate: workflow.On(workflow.Inspect, um.preSiteUpdateHandler),
	}
}

// preSiteUpdateHandler checks a site. Best-effort: an unreachable status service is not an error.
func (um *UnsupportedModules) preSiteUpdateHandler(evt *workflow.PreSiteUpdateEvent) error {

	modules, err := um.drush.GetUnsupportedModules(evt.Context(), evt.Path(), evt.Site())
	if err != nil {
		um.logger.Warn("failed to check for unsupported modules", zap.String("site", evt.Site()), zap.Error(err))
		return nil
	}
	if len(modules) == 0 {
		return nil
	}

	um.mu.Lock()
	for _, module := range modules {
		um.modules[module.Name] = module
	}
	um.mu.Unlock()

	um.logger.Info("unsupported modules found", zap.String("site", evt.Site()), zap.Int("count", len(modules)))

	return nil
}
