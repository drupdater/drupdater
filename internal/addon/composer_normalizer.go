package addon

import (
	"fmt"

	"github.com/drupdater/drupdater/internal"
	"github.com/drupdater/drupdater/internal/services"
	"github.com/gookit/event"
	"go.uber.org/zap"
)

type ComposerNormalizer struct {
	internal.BasicAddon
	logger   *zap.Logger
	composer Composer
}

func NewComposerNormalizer(logger *zap.Logger, composer Composer) *ComposerNormalizer {
	return &ComposerNormalizer{
		logger:   logger,
		composer: composer,
	}
}

func (cn *ComposerNormalizer) SubscribedEvents() map[string]any {
	return map[string]any{
		"post-composer-update": event.ListenerItem{
			Priority: event.Min,
			Listener: event.ListenerFunc(cn.postComposerUpdateHandler),
		},
	}
}

// RenderTemplate is empty: normalizing composer.json is visible in the diff, not in a section.
func (cn *ComposerNormalizer) RenderTemplate() (string, error) {
	return "", nil
}

func (cn *ComposerNormalizer) postComposerUpdateHandler(e event.Event) error {
	evt := e.(*services.PostComposerUpdateEvent)

	installed, err := cn.composer.IsPackageInstalled(evt.Context(), evt.Path(), "ergebnis/composer-normalize")
	if err != nil {
		return fmt.Errorf("failed to check if composer-normalize is installed: %w", err)
	}
	if !installed {
		cn.logger.Warn("composer-normalize not installed, skipping")
		return nil
	}

	_, err = cn.composer.Normalize(evt.Context(), evt.Path())
	if err != nil {
		return fmt.Errorf("failed to normalize composer.json: %w", err)
	}

	return nil
}
