package addon

import (
	"context"
	"fmt"

	"github.com/drupdater/drupdater/internal/composer"
	"github.com/drupdater/drupdater/internal/workflow"
	"go.uber.org/zap"
)

// NormalizerComposer runs composer normalize where the plugin is installed.
type NormalizerComposer interface {
	Project(ctx context.Context, dir string) (composer.Project, error)
	Normalize(ctx context.Context, dir string) (string, error)
}

type ComposerNormalizer struct {
	logger   *zap.Logger
	composer NormalizerComposer
}

func NewComposerNormalizer(logger *zap.Logger, composer NormalizerComposer) *ComposerNormalizer {
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

	project, err := cn.composer.Project(evt.Context(), evt.Path())
	if err != nil {
		return fmt.Errorf("failed to check if composer-normalize is installed: %w", err)
	}
	if !project.Installed("ergebnis/composer-normalize") {
		cn.logger.Warn("composer-normalize not installed, skipping")
		return nil
	}

	_, err = cn.composer.Normalize(evt.Context(), evt.Path())
	if err != nil {
		return fmt.Errorf("failed to normalize composer.json: %w", err)
	}

	return nil
}
