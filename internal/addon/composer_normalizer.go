package addon

import (
	"github.com/drupdater/drupdater/internal"
	"github.com/drupdater/drupdater/internal/services"
	"github.com/drupdater/drupdater/pkg/composer"
	"github.com/gookit/event"
	"go.uber.org/zap"
)

type ComposerNormalizer struct {
	internal.BasicAddon
	logger   *zap.Logger
	composer composer.Runner
}

func NewComposerNormalizer(logger *zap.Logger, composer composer.Runner) *ComposerNormalizer {
	return &ComposerNormalizer{
		logger:   logger,
		composer: composer,
	}
}

func (h *ComposerNormalizer) SubscribedEvents() map[string]interface{} {
	return map[string]interface{}{
		"post-composer-update": event.ListenerItem{
			Priority: event.Min,
			Listener: event.ListenerFunc(h.postComposerUpdateHandler),
		},
	}
}

func (h *ComposerNormalizer) RenderTemplate() (string, error) {
	return "", nil
}

func (h *ComposerNormalizer) postComposerUpdateHandler(e event.Event) error {
	event := e.(*services.PostComposerUpdateEvent)

	installed, err := h.composer.IsPackageInstalled(event.Context(), event.Path(), "ergebnis/composer-normalize")
	if err != nil {
		return err
	}
	if !installed {
		h.logger.Warn("composer-normalize not installed, skipping")
		return nil
	}

	_, err = h.composer.Normalize(event.Context(), event.Path())
	return err
}
