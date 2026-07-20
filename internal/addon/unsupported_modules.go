package addon

import (
	"sort"
	"sync"

	"github.com/drupdater/drupdater/internal"
	"github.com/drupdater/drupdater/internal/services"
	"github.com/drupdater/drupdater/pkg/drush"
	"github.com/gookit/event"
	"go.uber.org/zap"
)

// UnsupportedModules detects installed modules that have reached end-of-life according to
// Drupal's update status service (status NOT_SUPPORTED): they have no supported upgrade path,
// which is a different risk category than a security vulnerability caught by composer_audit.
type UnsupportedModules struct {
	internal.BasicAddon
	logger *zap.Logger
	drush  Drush

	// mu guards modules: preSiteUpdateHandler runs concurrently for each site. Keyed by module
	// name so results are deduplicated across sites in multisite runs.
	mu      sync.Mutex
	modules map[string]drush.UnsupportedModule
}

// NewUnsupportedModules creates a new unsupported modules detector instance.
func NewUnsupportedModules(logger *zap.Logger, drushClient Drush) *UnsupportedModules {
	return &UnsupportedModules{
		logger:  logger,
		drush:   drushClient,
		modules: make(map[string]drush.UnsupportedModule),
	}
}

// SubscribedEvents returns the events this addon listens to.
func (um *UnsupportedModules) SubscribedEvents() map[string]any {
	return map[string]any{
		"pre-site-update": event.ListenerItem{
			Priority: event.Normal,
			Listener: event.ListenerFunc(um.preSiteUpdateHandler),
		},
	}
}

// RenderTemplate returns the rendered template for this addon.
func (um *UnsupportedModules) RenderTemplate() (string, error) {
	if len(um.modules) == 0 {
		return "", nil
	}

	modules := make([]drush.UnsupportedModule, 0, len(um.modules))
	for _, module := range um.modules {
		modules = append(modules, module)
	}
	sort.Slice(modules, func(i, j int) bool { return modules[i].Name < modules[j].Name })

	return um.Render("unsupported_modules.go.tmpl", modules)
}

// preSiteUpdateHandler checks a site for unsupported modules. This is a best-effort, informational
// check: failures (e.g. the update status service being unreachable) are logged and swallowed
// rather than aborting the run, since an unsupported module is reported, not treated as an error.
func (um *UnsupportedModules) preSiteUpdateHandler(e event.Event) error {
	evt := e.(*services.PreSiteUpdateEvent)

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
