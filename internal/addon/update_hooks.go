package addon

import (
	"fmt"

	"github.com/drupdater/drupdater/pkg/drush"
	"github.com/gookit/event"
	"go.uber.org/zap"
)

type UpdateHooks struct {
	BasicAddon
	logger *zap.Logger
	drush  drush.Runner

	hooks UpdateHooksPerSite
}

func NewUpdateHooks(logger *zap.Logger, drush drush.Runner) *UpdateHooks {
	return &UpdateHooks{
		logger: logger,
		drush:  drush,
		hooks:  make(UpdateHooksPerSite),
	}
}

func (h *UpdateHooks) SubscribedEvents() map[string]interface{} {
	return map[string]interface{}{
		"pre-site-update": event.ListenerItem{
			Priority: event.Min,
			Listener: event.ListenerFunc(h.preSiteUpdateHandler),
		},
	}
}

func (h *UpdateHooks) RenderTemplate() (string, error) {
	return h.Render("update_hooks.go.tmpl", h.hooks)
}

type UpdateHooksPerSite map[string]map[string]drush.UpdateHook

func (h *UpdateHooks) preSiteUpdateHandler(e event.Event) error {
	event := e.(*PreSiteUpdateEvent)

	hooks, err := h.drush.GetUpdateHooks(event.Context(), event.Path(), event.site)
	h.logger.Debug("update hooks", zap.Any("hooks", hooks))
	if err != nil {
		return fmt.Errorf("failed to get update hooks: %w", err)
	}
	if len(hooks) == 0 {
		h.logger.Debug("no update hooks found")
		return nil
	}
	h.hooks[event.site] = hooks

	return err
}
