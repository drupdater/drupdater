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
func (cd *ComposerDiff) SubscribedEvents() map[string]any {
	return map[string]any{
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

	// The run log gets the link-free table: the linked variant is what goes in the merge
	// request, but its markdown URLs make the same content unreadable in a terminal. A failure
	// here only costs a log line, so it must not fail the run — but it is worth reporting.
	plain, err := cd.composer.Diff(evt.Context(), evt.Path(), false)
	if err != nil {
		cd.logger.Warn("failed to render the dependency diff for the log", zap.Error(err))
		return nil
	}
	cd.logger.Info("dependency diff\n" + plain)

	return nil
}
