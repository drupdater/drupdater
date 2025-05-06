package allowplugins

import (
	"fmt"

	"github.com/drupdater/drupdater/internal/addon"
	"github.com/drupdater/drupdater/pkg/composer"
	"github.com/gookit/event"
	"go.uber.org/zap"
)

// DefaultAllowPlugins handles composer plugin management during updates
type DefaultAllowPlugins struct {
	addon.BasicAddon
	logger   *zap.Logger
	composer composer.Runner

	allowPlugins    map[string]bool
	newAllowPlugins []string
}

// NewDefaultAllowPlugins creates a new DefaultAllowPlugins instance
func NewDefaultAllowPlugins(logger *zap.Logger, composer composer.Runner) *DefaultAllowPlugins {
	return &DefaultAllowPlugins{
		logger:   logger,
		composer: composer,
	}
}

// SubscribedEvents returns the events this addon subscribes to
func (ap *DefaultAllowPlugins) SubscribedEvents() map[string]interface{} {
	return map[string]interface{}{
		"pre-composer-update": event.ListenerItem{
			Priority: event.Normal,
			Listener: event.ListenerFunc(ap.preComposerUpdateHandler),
		},
		"post-composer-update": event.ListenerItem{
			Priority: event.Normal,
			Listener: event.ListenerFunc(ap.postComposerUpdateHandler),
		},
	}
}

// RenderTemplate returns the rendered template for this addon
func (ap *DefaultAllowPlugins) RenderTemplate() (string, error) {
	return ap.Render("allowplugins.go.tmpl", struct {
		NewAllowPlugins []string
	}{
		NewAllowPlugins: ap.newAllowPlugins,
	})
}

func (ap *DefaultAllowPlugins) preComposerUpdateHandler(e event.Event) error {
	evt := e.(*addon.PreComposerUpdateEvent)

	var err error
	ap.allowPlugins, err = ap.composer.GetAllowPlugins(evt.Context(), evt.Path())
	if err != nil {
		return fmt.Errorf("failed to get composer allow plugins: %w", err)
	}

	// Allow all plugins during update
	return ap.composer.SetConfig(evt.Context(), evt.Path(), "allow-plugins", "true")
}

func (ap *DefaultAllowPlugins) postComposerUpdateHandler(e event.Event) error {
	evt := e.(*addon.PostComposerUpdateEvent)

	allPlugins, err := ap.composer.GetInstalledPlugins(evt.Context(), evt.Path())
	if err != nil {
		return err
	}

	// Add new plugins to allow-plugins
	for key := range allPlugins {
		if _, ok := ap.allowPlugins[key]; !ok {
			ap.allowPlugins[key] = false
			ap.newAllowPlugins = append(ap.newAllowPlugins, key)
		}
	}
	if err := ap.composer.SetAllowPlugins(evt.Context(), evt.Path(), ap.allowPlugins); err != nil {
		return err
	}

	return nil
}
