package addon

import (
	"fmt"

	"github.com/drupdater/drupdater/internal"
	"github.com/drupdater/drupdater/internal/services"
	"github.com/gookit/event"
	"go.uber.org/zap"
)

// ComposerDiff handles diffing composer dependency changes.
type ComposerDiff struct {
	internal.BasicAddon
	logger   *zap.Logger
	composer Composer

	table string
}

// NewComposerDiff creates a new composer diff instance.
func NewComposerDiff(logger *zap.Logger, composer Composer) *ComposerDiff {
	return &ComposerDiff{
		logger:   logger,
		composer: composer,
	}
}

// SubscribedEvents returns the events this addon listens to.
func (cd *ComposerDiff) SubscribedEvents() map[string]interface{} {
	return map[string]interface{}{
		"post-composer-update": event.ListenerItem{
			Priority: event.Min,
			Listener: event.ListenerFunc(cd.postComposerUpdateHandler),
		},
	}
}

// RenderTemplate returns the rendered template for this addon.
func (cd *ComposerDiff) RenderTemplate() (string, error) {
	return cd.Render("composer_diff.go.tmpl", cd.table)
}

func (cd *ComposerDiff) postComposerUpdateHandler(e event.Event) error {
	evt := e.(*services.PostComposerUpdateEvent)

	table, err := cd.composer.Diff(evt.Context(), evt.Path(), true)
	if err != nil {
		return fmt.Errorf("failed to get diff: %w", err)
	}
	cd.table = table

	cd.logger.Sugar().Info("composer diff table", fmt.Sprintf("\n%s", table))

	return nil
}
