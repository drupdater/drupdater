package addon

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/drupdater/drupdater/internal/composer"
	"github.com/drupdater/drupdater/internal/workflow"
	"go.uber.org/zap"
)

// AllowPluginsComposer reads and rewrites composer.json's allow-plugins list.
type AllowPluginsComposer interface {
	GetAllowPlugins(dir string) (composer.AllowPlugins, error)
	GetInstalledPlugins(ctx context.Context, dir string) (map[string]any, error)
	SetConfig(ctx context.Context, dir string, key string, value string) error
	UnsetConfig(ctx context.Context, dir string, key string) error
}

// ComposerAllowPlugins lets an unattended update install plugins Composer would otherwise block,
// and records each new one as denied for a human to decide on.
type ComposerAllowPlugins struct {
	logger   *zap.Logger
	composer AllowPluginsComposer

	original composer.AllowPlugins
	// wildcard is set while the temporary trailing "*": true rule is in composer.json.
	wildcard        bool
	newAllowPlugins []string
}

func NewComposerAllowPlugins(logger *zap.Logger, composer AllowPluginsComposer) *ComposerAllowPlugins {
	return &ComposerAllowPlugins{
		logger:   logger,
		composer: composer,
	}
}

func (ap *ComposerAllowPlugins) Hooks() workflow.Hooks {
	return workflow.Hooks{
		PreComposerUpdate:  workflow.Propose(workflow.Change, ap.preComposerUpdateHandler),
		PostComposerUpdate: workflow.On(workflow.Change, ap.postComposerUpdateHandler),
	}
}

// preComposerUpdateHandler appends a "*": true rule, and proposes nothing about the update itself.
// Composer applies the first matching rule, so the project's own rules, denials included, still
// decide every plugin they name.
func (ap *ComposerAllowPlugins) preComposerUpdateHandler(evt *workflow.PreComposerUpdateEvent) (workflow.UpdatePlan, error) {
	var err error
	ap.original, err = ap.composer.GetAllowPlugins(evt.Path())
	if err != nil {
		return workflow.UpdatePlan{}, fmt.Errorf("failed to get composer allow plugins: %w", err)
	}

	if _, hasWildcard := ap.original.Rules["*"]; ap.original.All != nil || hasWildcard {
		return workflow.UpdatePlan{}, nil
	}
	if err := ap.composer.SetConfig(evt.Context(), evt.Path(), "allow-plugins.*", "true"); err != nil {
		return workflow.UpdatePlan{}, fmt.Errorf("failed to allow plugins for the update: %w", err)
	}
	ap.wildcard = true
	return workflow.UpdatePlan{}, nil
}

// postComposerUpdateHandler denies each plugin no rule of the project's decides, then removes the
// wildcard, so composer.json ends as it began plus one explicit entry per new plugin. In that order
// because Composer refuses to run at all while an installed plugin is undecided.
func (ap *ComposerAllowPlugins) postComposerUpdateHandler(evt *workflow.PostComposerUpdateEvent) error {
	ctx, path := evt.Context(), evt.Path()

	installed, err := ap.composer.GetInstalledPlugins(ctx, path)
	if err != nil {
		return err
	}

	for _, plugin := range slices.Sorted(maps.Keys(installed)) {
		if ap.original.Decides(plugin) {
			continue
		}
		if err := ap.composer.SetConfig(ctx, path, "allow-plugins."+plugin, "false"); err != nil {
			return fmt.Errorf("failed to deny new plugin %s: %w", plugin, err)
		}
		ap.newAllowPlugins = append(ap.newAllowPlugins, plugin)
	}

	if !ap.wildcard {
		return nil
	}
	if err := ap.composer.UnsetConfig(ctx, path, "allow-plugins.*"); err != nil {
		return fmt.Errorf("failed to remove the temporary plugin rule: %w", err)
	}
	ap.wildcard = false

	// Removing the wildcard leaves {} where the project had no setting at all.
	if !ap.original.Set && len(ap.newAllowPlugins) == 0 {
		if err := ap.composer.UnsetConfig(ctx, path, "allow-plugins"); err != nil {
			return fmt.Errorf("failed to remove the temporary plugin config: %w", err)
		}
	}

	return nil
}
