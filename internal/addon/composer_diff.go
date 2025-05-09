package addon

import (
	"fmt"

	"github.com/drupdater/drupdater/internal"
	"github.com/drupdater/drupdater/internal/services"
	"github.com/drupdater/drupdater/pkg/composer"
	"github.com/gookit/event"
	"go.uber.org/zap"
)

type ComposerDiff struct {
	internal.BasicAddon
	logger   *zap.Logger
	composer composer.Runner

	table string
}

func NewComposerDiff(logger *zap.Logger, composer composer.Runner) *ComposerDiff {
	return &ComposerDiff{
		logger:   logger,
		composer: composer,
	}
}

func (h *ComposerDiff) SubscribedEvents() map[string]interface{} {
	return map[string]interface{}{
		"post-composer-update": event.ListenerItem{
			Priority: event.Min,
			Listener: event.ListenerFunc(h.postComposerUpdateHandler),
		},
	}
}

func (h *ComposerDiff) RenderTemplate() (string, error) {
	return h.Render("composer_diff.go.tmpl", h.table)
}

func (h *ComposerDiff) postComposerUpdateHandler(e event.Event) error {
	event := e.(*services.PostComposerUpdateEvent)

	table, err := h.composer.Diff(event.Context(), event.Path(), true)
	if err != nil {
		return fmt.Errorf("failed to get diff: %w", err)
	}
	h.table = table

	h.logger.Sugar().Info("composer diff table", fmt.Sprintf("\n%s", table))

	return err
}
