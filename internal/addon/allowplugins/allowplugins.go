package allowplugins

import (
	"fmt"

	"github.com/drupdater/drupdater/internal/addon"
	"github.com/drupdater/drupdater/pkg/composer"
	"github.com/gookit/event"
	"go.uber.org/zap"
)

type DefaultAllowPlugins struct {
	addon.BasicAddon
	logger   *zap.Logger
	composer composer.Runner

	allowPlugins    map[string]bool
	newAllowPlugins []string
}

func NewDefaultAllowPlugins(logger *zap.Logger, composer composer.Runner) *DefaultAllowPlugins {
	return &DefaultAllowPlugins{
		logger:   logger,
		composer: composer,
	}
}

func (h *DefaultAllowPlugins) SubscribedEvents() map[string]interface{} {
	return map[string]interface{}{
		"pre-composer-update": event.ListenerItem{
			Priority: event.Normal,
			Listener: event.ListenerFunc(h.preComposerUpdateHandler),
		},
		"post-composer-update": event.ListenerItem{
			Priority: event.Normal,
			Listener: event.ListenerFunc(h.postComposerUpdateHandler),
		},
	}
}

func (h *DefaultAllowPlugins) RenderTemplate() (string, error) {
	return h.Render("allowplugins.go.tmpl", struct {
		NewAllowPlugins []string
	}{
		NewAllowPlugins: h.newAllowPlugins,
	})
}

func (h *DefaultAllowPlugins) preComposerUpdateHandler(e event.Event) error {

	event := e.(*addon.PreComposerUpdateEvent)

	var err error
	h.allowPlugins, err = h.composer.GetAllowPlugins(event.Ctx, event.Path)
	if err != nil {
		return fmt.Errorf("failed to get composer allow plugins: %w", err)
	}

	// Allow all plugins during update
	return h.composer.SetConfig(event.Ctx, event.Path, "allow-plugins", "true")
}

func (h *DefaultAllowPlugins) postComposerUpdateHandler(e event.Event) error {

	event := e.(*addon.PostComposerUpdateEvent)

	allPlugins, err := h.composer.GetInstalledPlugins(event.Ctx, event.Path)
	if err != nil {
		return err
	}

	// Add new plugins to allow-plugins
	for key := range allPlugins {
		if _, ok := h.allowPlugins[key]; !ok {
			h.allowPlugins[key] = false
			h.newAllowPlugins = append(h.newAllowPlugins, key)
		}
	}
	if err := h.composer.SetAllowPlugins(event.Ctx, event.Path, h.allowPlugins); err != nil {
		return err
	}

	return nil
}
