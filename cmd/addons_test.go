package cmd

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/drupdater/drupdater/internal/agent"
	"github.com/drupdater/drupdater/internal/config"
	"github.com/drupdater/drupdater/internal/workflow"
)

// agentCatalog is the catalogue the agent answers drupdater:addons with: agent/protocol's
// example, which the agent's own suite pins to the real one.
var agentCatalog = func() addonCatalog {
	raw, err := os.ReadFile(filepath.Join("..", "agent", "protocol", "addons", "response.json"))
	if err != nil {
		panic(err)
	}
	var reply struct {
		Result struct {
			Addons []agent.Addon `json:"addons"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &reply); err != nil {
		panic(err)
	}
	catalog, err := newAddonCatalog(reply.Result.Addons)
	if err != nil {
		panic(err)
	}
	return catalog
}()

func names(addons []workflow.Addon) []string {
	var out []string
	for _, a := range addons {
		out = append(out, a.Name)
	}
	return out
}

func TestTheCatalogueReadsTheAgentsAddonsAndTheirStages(t *testing.T) {
	assert.Equal(t, []string{"composer_allow_plugins", "composer_patches", "composer_diff", "update_hooks", "composer_audit", "unsupported_modules"}, agentCatalog.mandatory)
	assert.Equal(t, []string{"code_beautifier", "deprecations_remover", "translations_updater", "composer_normalizer"}, agentCatalog.defaults)
	assert.Equal(t, workflow.Addon{Name: "composer_audit", Hooks: map[string]workflow.Stage{
		workflow.EventPreComposerUpdate: workflow.Prepare,
		workflow.EventPostCodeUpdate:    workflow.Inspect,
	}}, agentCatalog.addons[4])
}

func TestACatalogueThePipelineCannotRunIsRefused(t *testing.T) {
	for name, tt := range map[string]struct {
		addons []agent.Addon
		want   string
	}{
		"an addon twice":   {[]agent.Addon{{Name: "a"}, {Name: "a"}}, `the agent's catalogue lists "a" twice`},
		"an unknown event": {[]agent.Addon{{Name: "a", Hooks: map[string]string{"post-everything": "change"}}}, `addon "a" hooks the unknown event "post-everything"`},
		"an unknown stage": {[]agent.Addon{{Name: "a", Hooks: map[string]string{"post-code-update": "later"}}}, `addon "a" hooks post-code-update: unknown stage "later"`},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := newAddonCatalog(tt.addons)
			require.EqualError(t, err, tt.want)
		})
	}
}

func TestANormalRunNamingNoAddonsGetsTheMandatoryOnesThenTheDefaults(t *testing.T) {
	addons, err := agentCatalog.create(config.Config{})

	require.NoError(t, err)
	assert.Equal(t, append(append([]string{}, agentCatalog.mandatory...), agentCatalog.defaults...), names(addons))
}

func TestASecurityRunNamingNoAddonsGetsTheMandatoryOnesOnly(t *testing.T) {
	addons, err := agentCatalog.create(config.Config{Security: true})

	require.NoError(t, err)
	assert.Equal(t, agentCatalog.mandatory, names(addons))
}

func TestARunGetsTheAddonsItsRunTypeNamesAfterTheMandatoryOnesOnceEach(t *testing.T) {
	cfg := config.Config{Security: true, RunTypes: config.RunTypesConfig{
		Normal:   config.RunTypeConfig{Addons: []string{"code_beautifier"}},
		Security: config.RunTypeConfig{Addons: []string{"composer_normalizer", "composer_audit", "composer_normalizer"}},
	}}

	addons, err := agentCatalog.create(cfg)

	require.NoError(t, err)
	assert.Equal(t, append(append([]string{}, agentCatalog.mandatory...), "composer_normalizer"), names(addons))
}

func TestARunNamingNoneOnPurposeGetsTheMandatoryOnesOnly(t *testing.T) {
	addons, err := agentCatalog.create(config.Config{RunTypes: config.RunTypesConfig{Normal: config.RunTypeConfig{Addons: []string{}}}})

	require.NoError(t, err)
	assert.Equal(t, agentCatalog.mandatory, names(addons))
}

func TestCreatingAnUnknownAddonFails(t *testing.T) {
	_, err := agentCatalog.create(config.Config{RunTypes: config.RunTypesConfig{Normal: config.RunTypeConfig{Addons: []string{"nope"}}}})

	require.EqualError(t, err, `unknown addon "nope"`)
}

func TestTheAddonsCommandListsTheConfigurableAddonsMarkingTheDefaults(t *testing.T) {
	out, err := executeTree(t, testApp(), "addons")

	require.NoError(t, err)
	assert.Contains(t, out, "  code_beautifier (default)\n")
	assert.Contains(t, out, "  composer_normalizer (default)\n")
	assert.Contains(t, out, "Addons you can set under run_types.normal.addons")
	for _, mandatory := range agentCatalog.mandatory {
		assert.NotContains(t, out, mandatory, "a mandatory addon is not for the project to set")
	}
}

func TestTheAddonsCommandFailsWithoutAnAgentToAsk(t *testing.T) {
	a := testApp()
	a.addonCatalog = func(context.Context, *zap.Logger) (addonCatalog, error) { return addonCatalog{}, assert.AnError }

	_, err := executeTree(t, a, "addons")

	require.ErrorIs(t, err, assert.AnError)
}

func TestTheAgentsCatalogueIsAskedOfComposer(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	_, err := agentAddonCatalog(t.Context(), zap.NewNop())

	require.ErrorContains(t, err, "failed to read the addon catalogue")
	assert.Contains(t, err.Error(), "composer")
}
