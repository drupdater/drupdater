package addon

import (
	"fmt"

	"github.com/drupdater/drupdater/internal"
	"github.com/drupdater/drupdater/internal/services"
	"github.com/drupdater/drupdater/pkg/drush"
	"github.com/gookit/event"
	"go.uber.org/zap"
)

// UpdateHooksPerSite maps site names to their update hooks
type UpdateHooksPerSite map[string]map[string]drush.UpdateHook

// UpdateHooks represents the update hooks addon
type UpdateHooks struct {
	internal.BasicAddon
	logger *zap.Logger
	drush  Drush

	hooks UpdateHooksPerSite
}

// NewUpdateHooks creates a new update hooks instance
func NewUpdateHooks(logger *zap.Logger, drush Drush) *UpdateHooks {
	return &UpdateHooks{
		logger: logger,
		drush:  drush,
		hooks:  make(UpdateHooksPerSite),
	}
}

// SubscribedEvents returns the events this addon listens to
func (uh *UpdateHooks) SubscribedEvents() map[string]interface{} {
	return map[string]interface{}{
		"pre-site-update": event.ListenerItem{
			Priority: event.Min,
			Listener: event.ListenerFunc(uh.preSiteUpdateHandler),
		},
	}
}

// RenderTemplate returns the rendered template for this addon
func (uh *UpdateHooks) RenderTemplate() (string, error) {
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
	uh.hooks[evt.Site()] = hooks

	return nil
}
