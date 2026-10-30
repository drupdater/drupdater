package addon

import (
	"reflect"
	"sync"
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
	if h.PreComposerUpdate != nil {
		add("pre-composer-update", &h.PreComposerUpdate.Stage)
	}
	add("post-composer-update", stageOf(h.PostComposerUpdate))
	add("post-code-update", stageOf(h.PostCodeUpdate))
	add("pre-site-update", stageOf(h.PreSiteUpdate))
	add("post-site-update", stageOf(h.PostSiteUpdate))
	return got
}

func stageOf[E any](h *workflow.Hook[E]) *workflow.Stage {
	if h == nil {
		return nil
	}
	return &h.Stage
}

// everyAddon is each addon with the stage of every hook it has: the ordering contract between
// addons, which docs/explanation/addon-architecture.md explains.
var everyAddon = map[string]struct {
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
		"pre-composer-update": workflow.Prepare,
		"post-code-update":    workflow.Inspect,
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
		"pre-site-update": workflow.Inspect,
	}},
	"update_hooks": {&UpdateHooks{}, map[string]workflow.Stage{
		"pre-site-update": workflow.Inspect,
	}},
}

func TestAddonHookStages(t *testing.T) {
	for name, tt := range everyAddon {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.want, stages(tt.addon.Hooks()))
		})
	}
}

// Per-site hooks fire concurrently, so an addon with one keeps what they find in a
// workflow.PerSite and nowhere else: a map, slice or mutex of its own is how a shared write
// goes unguarded, and -race only notices when a test happens to overlap two sites.
func TestPerSiteHooksShareStateOnlyThroughPerSite(t *testing.T) {
	for name, tt := range everyAddon {
		hooks := tt.addon.Hooks()
		if hooks.PreSiteUpdate == nil && hooks.PostSiteUpdate == nil {
			continue
		}
		t.Run(name, func(t *testing.T) {
			addonType := reflect.TypeOf(tt.addon).Elem()
			for field := range addonType.Fields() {
				switch {
				case field.Type.Kind() == reflect.Map, field.Type.Kind() == reflect.Slice:
					t.Errorf("%s.%s is a %s: keep per-site findings in a workflow.PerSite", addonType.Name(), field.Name, field.Type.Kind())
				case field.Type == reflect.TypeFor[sync.Mutex](), field.Type == reflect.TypeFor[sync.RWMutex]():
					t.Errorf("%s.%s is a mutex: workflow.PerSite does the locking", addonType.Name(), field.Name)
				}
			}
		})
	}
}

func TestNoTwoAddonsShareAToolInterface(t *testing.T) {
	owner := map[reflect.Type]string{}
	for name, tt := range everyAddon {
		for field := range reflect.TypeOf(tt.addon).Elem().Fields() {
			if field.Type.Kind() != reflect.Interface || field.Type.PkgPath() != reflect.TypeFor[CodeBeautifier]().PkgPath() {
				continue
			}
			if other, ok := owner[field.Type]; ok && other != name {
				t.Errorf("%s and %s both depend on %s: give each addon its own interface", other, name, field.Type.Name())
			}
			owner[field.Type] = name
		}
	}
	assert.NotEmpty(t, owner, "no addon has a tool interface, so the check above checked nothing")
}
