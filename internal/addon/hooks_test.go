package addon

import (
	"testing"

	"github.com/drupdater/drupdater/internal/workflow"
	"github.com/stretchr/testify/assert"
)

func stages(h workflow.Hooks) map[string]workflow.Stage {
	got := map[string]workflow.Stage{}
	add := func(name string, stage *workflow.Stage) {
		if stage != nil {
			got[name] = *stage
		}
	}
	add("pre-composer-update", stageOf(h.PreComposerUpdate))
	add("post-composer-update", stageOf(h.PostComposerUpdate))
	add("post-code-update", stageOf(h.PostCodeUpdate))
	add("pre-site-update", stageOf(h.PreSiteUpdate))
	add("post-site-update", stageOf(h.PostSiteUpdate))
	add("pre-merge-request-create", stageOf(h.PreMergeRequestCreate))
	return got
}

func stageOf[E any](h *workflow.Hook[E]) *workflow.Stage {
	if h == nil {
		return nil
	}
	return &h.Stage
}

// The stages are the ordering contract between addons; docs/explanation/addon-architecture.md
// explains each one.
func TestAddonHookStages(t *testing.T) {
	tests := map[string]struct {
		addon workflow.Addon
		want  map[string]workflow.Stage
	}{
		"code_beautifier": {&CodeBeautifier{}, map[string]workflow.Stage{
			"post-code-update": workflow.Format,
		}},
		"composer_allow_plugins": {&ComposerAllowPlugins{}, map[string]workflow.Stage{
			"pre-composer-update":  workflow.Change,
			"post-composer-update": workflow.Change,
		}},
		"composer_audit": {&ComposerAudit{}, map[string]workflow.Stage{
			"pre-composer-update":      workflow.Prepare,
			"post-code-update":         workflow.Inspect,
			"pre-merge-request-create": workflow.Prepare,
		}},
		"composer_diff": {&ComposerDiff{}, map[string]workflow.Stage{
			"post-composer-update": workflow.Inspect,
		}},
		"composer_normalizer": {&ComposerNormalizer{}, map[string]workflow.Stage{
			"post-composer-update": workflow.Format,
		}},
		"composer_patches": {&ComposerPatches1{}, map[string]workflow.Stage{
			"pre-composer-update": workflow.Change,
		}},
		"deprecations_remover": {&DeprecationsRemover{}, map[string]workflow.Stage{
			"post-code-update": workflow.Change,
		}},
		"translations_updater": {&TranslationsUpdater{}, map[string]workflow.Stage{
			"post-site-update": workflow.Change,
		}},
		"unsupported_modules": {&UnsupportedModules{}, map[string]workflow.Stage{
			"pre-site-update":          workflow.Inspect,
			"pre-merge-request-create": workflow.Inspect,
		}},
		"update_hooks": {&UpdateHooks{}, map[string]workflow.Stage{
			"pre-site-update": workflow.Inspect,
		}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.want, stages(tt.addon.Hooks()))
		})
	}
}
