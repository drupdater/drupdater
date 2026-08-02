package addon

import (
	"fmt"
	"sync"

	"github.com/drupdater/drupdater/internal"
	"github.com/drupdater/drupdater/internal/services"
	"github.com/drupdater/drupdater/pkg/drush"
	"github.com/gookit/event"
	"go.uber.org/zap"
)

type UpdateHooksPerSite map[string]map[string]drush.UpdateHook

type UpdateHooks struct {
	internal.BasicAddon
	logger *zap.Logger
	drush  Drush

	// mu guards hooks: preSiteUpdateHandler runs concurrently for each site.
	mu    sync.Mutex
	hooks UpdateHooksPerSite
}

func NewUpdateHooks(logger *zap.Logger, drush Drush) *UpdateHooks {
	return &UpdateHooks{
		logger: logger,
		drush:  drush,
		hooks:  make(UpdateHooksPerSite),
	}
}

func (uh *UpdateHooks) SubscribedEvents() map[string]any {
	return map[string]any{
		"pre-site-update": event.ListenerItem{
			Priority: event.Min,
			Listener: event.ListenerFunc(uh.preSiteUpdateHandler),
		},
	}
}

// RenderTemplate locks even though errgroup.Wait already orders every site's goroutine before
// this call: that ordering is the caller's invariant, not this type's.
func (uh *UpdateHooks) RenderTemplate() (string, error) {
	uh.mu.Lock()
	defer uh.mu.Unlock()

	if len(uh.hooks) == 0 {
		return "", nil
	}
	return uh.Render("update_hooks.go.tmpl", uh.hooks)
}

func (uh *UpdateHooks) preSiteUpdateHandler(e event.Event) error {
	evt := e.(*services.PreSiteUpdateEvent)

	hooks, err := uh.drush.GetUpdateHooks(evt.Context(), evt.Path(), evt.Site())
	uh.logger.Debug("update hooks", zap.Any("hooks", hooks))
	if err != nil {
		return fmt.Errorf("failed to get update hooks: %w", err)
	}
	if len(hooks) == 0 {
		uh.logger.Debug("no update hooks found")
		return nil
	}
	uh.mu.Lock()
	uh.hooks[evt.Site()] = hooks
	uh.mu.Unlock()
	uh.logger.Info("update hooks found", zap.String("site", evt.Site()), zap.Int("count", len(hooks)))

	return nil
}
