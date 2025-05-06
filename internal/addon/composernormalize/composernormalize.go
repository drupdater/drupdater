package composernormalize

import (
	"github.com/drupdater/drupdater/internal/addon"
	"github.com/drupdater/drupdater/pkg/composer"
	"github.com/gookit/event"
	"go.uber.org/zap"
)

type DefaultComposerNormalize struct {
	addon.BasicAddon
	logger   *zap.Logger
	composer composer.Runner
}

func NewDefaultComposerNormalize(logger *zap.Logger, composer composer.Runner) *DefaultComposerNormalize {
	return &DefaultComposerNormalize{
		logger:   logger,
		composer: composer,
	}
}

func (h *DefaultComposerNormalize) SubscribedEvents() map[string]interface{} {
	return map[string]interface{}{
		"post-composer-update": event.ListenerItem{
			Priority: event.Max,
			Listener: event.ListenerFunc(h.postComposerUpdateHandler),
		},
	}
}

func (h *DefaultComposerNormalize) RenderTemplate() (string, error) {
	return "", nil
}

func (h *DefaultComposerNormalize) postComposerUpdateHandler(e event.Event) error {
	event := e.(*addon.PostComposerUpdateEvent)

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
