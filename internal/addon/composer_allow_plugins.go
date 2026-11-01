package addon

import (
	"context"

	"github.com/drupdater/drupdater/internal/composer"
	"github.com/drupdater/drupdater/internal/workflow"
	"go.uber.org/zap"
)

// AllowPluginsComposer opens composer.json's allow-plugins for the update and closes it after.
type AllowPluginsComposer interface {
	OpenAllowPlugins(ctx context.Context, dir string) (composer.PluginsOpened, error)
	CloseAllowPlugins(ctx context.Context, dir string, opened composer.PluginsOpened) ([]string, error)
}

// ComposerAllowPlugins lets an unattended update install plugins Composer would otherwise block,
// and records each new one as denied for a human to decide on.
type ComposerAllowPlugins struct {
	logger   *zap.Logger
	composer AllowPluginsComposer
}

func NewComposerAllowPlugins(logger *zap.Logger, composer AllowPluginsComposer) *ComposerAllowPlugins {
	return &ComposerAllowPlugins{
		logger:   logger,
		composer: composer,
	}
}

func (ap *ComposerAllowPlugins) Hooks() workflow.Hooks {
	return workflow.Hooks{
		PreComposerUpdate:  workflow.ProduceProposal(workflow.Change, ap.preComposerUpdateHandler),
		PostComposerUpdate: workflow.ProduceFrom(workflow.Change, ap.postComposerUpdateHandler),
	}
}

// preComposerUpdateHandler opens allow-plugins, and proposes nothing about the update itself.
func (ap *ComposerAllowPlugins) preComposerUpdateHandler(evt *workflow.PreComposerUpdateEvent) (workflow.Proposal[composer.PluginsOpened], error) {
	opened, err := ap.composer.OpenAllowPlugins(evt.Context(), evt.Path())
	if err != nil {
		return workflow.Proposal[composer.PluginsOpened]{}, err
	}
	return workflow.Proposal[composer.PluginsOpened]{Finding: opened}, nil
}

// postComposerUpdateHandler closes allow-plugins again, so composer.json ends as it began plus one
// explicit denial per new plugin. Its finding is the plugins it denied, sorted.
func (ap *ComposerAllowPlugins) postComposerUpdateHandler(evt *workflow.PostComposerUpdateEvent, earlier *workflow.Findings) (workflow.Result[[]string], error) {
	recorded, _ := workflow.Shared[composer.PluginsOpened](earlier, workflow.EventPreComposerUpdate)
	denied, err := ap.composer.CloseAllowPlugins(evt.Context(), evt.Path(), recorded.Finding)
	if err != nil {
		return workflow.Result[[]string]{}, err
	}
	return workflow.Result[[]string]{Finding: denied}, nil
}
