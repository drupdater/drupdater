package addon

import (
	"context"
	"fmt"

	"github.com/drupdater/drupdater/internal/composer"
	"github.com/drupdater/drupdater/internal/workflow"
	"go.uber.org/zap"
)

// AllowPluginsComposer reads and rewrites composer.json's allow-plugins list.
type AllowPluginsComposer interface {
	Project(ctx context.Context, dir string) (composer.Project, error)
	SetConfig(ctx context.Context, dir string, key string, value string) error
	UnsetConfig(ctx context.Context, dir string, key string) error
}

// ComposerAllowPlugins lets an unattended update install plugins Composer would otherwise block,
// and records each new one as denied for a human to decide on.
type ComposerAllowPlugins struct {
	logger   *zap.Logger
	composer AllowPluginsComposer
}

// allowPluginsBefore is what the pre-update hook leaves for the post-update one to undo.
type allowPluginsBefore struct {
	original composer.AllowPlugins
	// wildcard is set when the hook added the temporary trailing "*": true rule.
	wildcard bool
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

// preComposerUpdateHandler appends a "*": true rule, and proposes nothing about the update itself.
// Composer applies the first matching rule, so the project's own rules, denials included, still
// decide every plugin they name.
func (ap *ComposerAllowPlugins) preComposerUpdateHandler(evt *workflow.PreComposerUpdateEvent) (workflow.Proposal[allowPluginsBefore], error) {
	project, err := ap.composer.Project(evt.Context(), evt.Path())
	if err != nil {
		return workflow.Proposal[allowPluginsBefore]{}, fmt.Errorf("failed to get composer allow plugins: %w", err)
	}
	original, err := composer.ParseAllowPlugins(project.AllowPlugins)
	if err != nil {
		return workflow.Proposal[allowPluginsBefore]{}, fmt.Errorf("failed to get composer allow plugins: %w", err)
	}

	if _, hasWildcard := original.Rules["*"]; original.All != nil || hasWildcard {
		return workflow.Proposal[allowPluginsBefore]{Finding: allowPluginsBefore{original: original}}, nil
	}
	if err := ap.composer.SetConfig(evt.Context(), evt.Path(), "allow-plugins.*", "true"); err != nil {
		return workflow.Proposal[allowPluginsBefore]{}, fmt.Errorf("failed to allow plugins for the update: %w", err)
	}
	return workflow.Proposal[allowPluginsBefore]{Finding: allowPluginsBefore{original: original, wildcard: true}}, nil
}

// postComposerUpdateHandler denies each plugin no rule of the project's decides, then removes the
// wildcard, so composer.json ends as it began plus one explicit entry per new plugin. In that order
// because Composer refuses to run at all while an installed plugin is undecided. Its finding is
// the plugins it denied, sorted.
func (ap *ComposerAllowPlugins) postComposerUpdateHandler(evt *workflow.PostComposerUpdateEvent, earlier *workflow.Findings) (workflow.Result[[]string], error) {
	ctx, path := evt.Context(), evt.Path()
	recorded, _ := workflow.Shared[allowPluginsBefore](earlier, workflow.EventPreComposerUpdate)
	before := recorded.Finding

	project, err := ap.composer.Project(ctx, path)
	if err != nil {
		return workflow.Result[[]string]{}, err
	}

	var denied []string
	for _, plugin := range project.Plugins() {
		if before.original.Decides(plugin) {
			continue
		}
		if err := ap.composer.SetConfig(ctx, path, "allow-plugins."+plugin, "false"); err != nil {
			return workflow.Result[[]string]{}, fmt.Errorf("failed to deny new plugin %s: %w", plugin, err)
		}
		denied = append(denied, plugin)
	}

	if before.wildcard {
		if err := ap.composer.UnsetConfig(ctx, path, "allow-plugins.*"); err != nil {
			return workflow.Result[[]string]{}, fmt.Errorf("failed to remove the temporary plugin rule: %w", err)
		}
		// Removing the wildcard leaves {} where the project had no setting at all.
		if !before.original.Set && len(denied) == 0 {
			if err := ap.composer.UnsetConfig(ctx, path, "allow-plugins"); err != nil {
				return workflow.Result[[]string]{}, fmt.Errorf("failed to remove the temporary plugin config: %w", err)
			}
		}
	}

	return workflow.Result[[]string]{Finding: denied}, nil
}
