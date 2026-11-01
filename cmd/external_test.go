package cmd

import (
	"bytes"
	"testing"

	"github.com/drupdater/drupdater/internal/config"
	"github.com/drupdater/drupdater/internal/external"
	"github.com/drupdater/drupdater/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type externalAddon struct {
	deps      external.Deps
	reportKey string
}

func (*externalAddon) Hooks() workflow.Hooks             { return workflow.Hooks{} }
func (a *externalAddon) ReportKey() string               { return a.reportKey }
func (*externalAddon) ReportData(*workflow.Findings) any { return nil }

func registration(name string) external.Registration {
	return external.Registration{Name: name, New: func(d external.Deps) workflow.Addon {
		return &externalAddon{deps: d, reportKey: name}
	}}
}

func TestExternalAddonsBecomeConfigurable(t *testing.T) {
	catalog, err := builtinAddons().withExternal([]external.Registration{registration("acme_tags")})

	require.NoError(t, err)
	assert.Contains(t, catalog.configurable(), "acme_tags")
	assert.Equal(t, builtinAddons().mandatory, catalog.mandatory)
	assert.NoError(t, catalog.validate(config.Config{RunTypes: config.RunTypesConfig{
		Security: config.RunTypeConfig{Addons: []string{"acme_tags"}},
	}}))
}

func TestExternalAddonsLeaveTheCatalogTheyExtendUnchanged(t *testing.T) {
	builtin := builtinAddons()

	_, err := builtin.withExternal([]external.Registration{registration("acme_tags")})

	require.NoError(t, err)
	assert.NotContains(t, builtin.factories, "acme_tags")
}

func TestExternalAddonIsBuiltWithTheRunsTools(t *testing.T) {
	catalog, err := builtinAddons().withExternal([]external.Registration{registration("acme_tags")})
	require.NoError(t, err)
	composer := newMockaddonComposer(t)
	drush := newMockaddonDrush(t)
	cfg := config.Config{Security: true, RunTypes: config.RunTypesConfig{
		Security: config.RunTypeConfig{Addons: []string{"acme_tags"}},
	}}

	addons, err := catalog.create(cfg, addonDeps{logger: zap.NewNop(), composer: composer, drush: drush})

	require.NoError(t, err)
	built, ok := addons[len(addons)-1].(*externalAddon)
	require.True(t, ok, "the external addon is built last, after the mandatory ones")
	assert.Same(t, composer, built.deps.Composer)
	assert.Same(t, drush, built.deps.Drush)
	assert.True(t, built.deps.Security)
	assert.NotNil(t, built.deps.Logger)
}

func TestExternalAddonRegistrationIsRefused(t *testing.T) {
	for name, reg := range map[string]external.Registration{
		"shadowing a builtin addon": registration("composer_diff"),
		"with an uppercase name":    registration("AcmeTags"),
		"with an empty name":        registration(""),
		"starting with a digit":     registration("1acme"),
		"with a dash":               registration("acme-tags"),
		"without a constructor":     {Name: "acme_tags"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := builtinAddons().withExternal([]external.Registration{reg})
			assert.Error(t, err)
		})
	}
}

func TestTwoExternalAddonsCannotShareAName(t *testing.T) {
	_, err := builtinAddons().withExternal([]external.Registration{registration("acme"), registration("acme")})

	require.EqualError(t, err, `addon "acme" is already registered`)
}

func TestAnAddonReportingUnderAnotherNameIsRefused(t *testing.T) {
	reg := external.Registration{Name: "acme", New: func(external.Deps) workflow.Addon {
		return &externalAddon{reportKey: "composer_audit"}
	}}
	catalog, err := builtinAddons().withExternal([]external.Registration{reg})
	require.NoError(t, err)

	_, err = catalog.create(config.Config{RunTypes: config.RunTypesConfig{
		Normal: config.RunTypeConfig{Addons: []string{"acme"}},
	}}, addonDeps{logger: zap.NewNop()})

	require.EqualError(t, err, `addon "acme" reports under "composer_audit": the report key must be the addon's name`)
}

func TestAnAddonBuildingNothingIsRefused(t *testing.T) {
	reg := external.Registration{Name: "acme", New: func(external.Deps) workflow.Addon { return nil }}
	catalog, err := builtinAddons().withExternal([]external.Registration{reg})
	require.NoError(t, err)

	_, err = catalog.create(config.Config{RunTypes: config.RunTypesConfig{
		Normal: config.RunTypeConfig{Addons: []string{"acme"}},
	}}, addonDeps{logger: zap.NewNop()})

	require.EqualError(t, err, `addon "acme" built nothing`)
}

func TestExecuteWithRefusesToStartOnAnInvalidRegistration(t *testing.T) {
	var stderr bytes.Buffer

	code := executeWith(t.Context(), &stderr, testApp(), []external.Registration{registration("Bad")}, []string{"addons"})

	assert.Equal(t, 1, code)
	assert.Contains(t, stderr.String(), "invalid addon registration")
}

func TestExecuteWithListsExternalAddons(t *testing.T) {
	var stderr bytes.Buffer
	app := testApp()

	code := executeWith(t.Context(), &stderr, app, []external.Registration{registration("acme_tags")}, []string{"addons", "--help"})
	assert.Equal(t, 0, code)

	out, err := executeTree(t, mustWithExternal(t, app, registration("acme_tags")), "addons")
	require.NoError(t, err)
	assert.Contains(t, out, "  acme_tags\n")
}

func mustWithExternal(t *testing.T, a app, regs ...external.Registration) app {
	t.Helper()
	catalog, err := a.addons.withExternal(regs)
	require.NoError(t, err)
	a.addons = catalog
	return a
}
