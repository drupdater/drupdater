package addon

import (
	"fmt"

	"github.com/drupdater/drupdater/internal/workflow"
	"go.uber.org/zap"
)

type ComposerNormalizer struct {
	logger   *zap.Logger
	composer Composer
}

func NewComposerNormalizer(logger *zap.Logger, composer Composer) *ComposerNormalizer {
	return &ComposerNormalizer{
		logger:   logger,
		composer: composer,
	}
}

func (cn *ComposerNormalizer) Hooks() workflow.Hooks {
	return workflow.Hooks{
		PostComposerUpdate: workflow.On(workflow.Format, cn.postComposerUpdateHandler),
	}
}

func (cn *ComposerNormalizer) postComposerUpdateHandler(evt *workflow.PostComposerUpdateEvent) error {

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
