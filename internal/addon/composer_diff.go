package addon

import (
	"fmt"

	"github.com/drupdater/drupdater/internal"
	"github.com/drupdater/drupdater/internal/services"
	"github.com/gookit/event"
	"go.uber.org/zap"
)

type ComposerDiff struct {
	internal.BasicAddon
	logger   *zap.Logger
	composer Composer

	table string
}

func NewComposerDiff(logger *zap.Logger, composer Composer) *ComposerDiff {
	return &ComposerDiff{
		logger:   logger,
		composer: composer,
	}
}

func (cd *ComposerDiff) SubscribedEvents() map[string]any {
	return map[string]any{
		"post-composer-update": event.ListenerItem{
			Priority: event.Min,
			Listener: event.ListenerFunc(cd.postComposerUpdateHandler),
		},
	}
}

// RenderTemplate returns "" with no diff, so the section is omitted rather than left empty.
func (cd *ComposerDiff) RenderTemplate() (string, error) {
	if cd.table == "" {
		return "", nil
	}
	return cd.Render("composer_diff.go.tmpl", cd.table)
}

func (cd *ComposerDiff) postComposerUpdateHandler(e event.Event) error {
	evt := e.(*services.PostComposerUpdateEvent)

	table, err := cd.composer.Diff(evt.Context(), evt.Path(), true)
	if err != nil {
		return fmt.Errorf("failed to get diff: %w", err)
	}
	cd.table = table

	// The log gets the link-free table: markdown URLs are unreadable in a terminal.
	plain, err := cd.composer.Diff(evt.Context(), evt.Path(), false)
	if err != nil {
		cd.logger.Warn("failed to render the dependency diff for the log", zap.Error(err))
		return nil
	}
	cd.logger.Info("dependency diff\n" + plain)

	return nil
}
