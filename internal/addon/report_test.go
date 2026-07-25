package addon

import (
	"testing"

	"github.com/drupdater/drupdater/internal/report"
	"github.com/drupdater/drupdater/pkg/composer"
	"github.com/drupdater/drupdater/pkg/drush"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Every addon contributing to the report must satisfy report.Reporter structurally, without
// importing the report package itself. If a signature drifts, this fails to compile.
var (
	_ report.Reporter = (*ComposerAudit)(nil)
	_ report.Reporter = (*UpdateHooks)(nil)
	_ report.Reporter = (*UnsupportedModules)(nil)
	_ report.Reporter = (*ComposerPatches1)(nil)
)

func TestComposerAuditReportData(t *testing.T) {
	before := composer.Advisory{CVE: "CVE-1", PackageName: "drupal/foo"}
	stillOpen := composer.Advisory{CVE: "CVE-2", PackageName: "drupal/bar"}

	ca := &ComposerAudit{
		beforeAudit: composer.Audit{Advisories: []composer.Advisory{before, stillOpen}},
		afterAudit:  composer.Audit{Advisories: []composer.Advisory{stillOpen}},
	}

	assert.Equal(t, "composer_audit", ca.ReportKey())

	data, ok := ca.ReportData().(SecurityAdvisories)
	require.True(t, ok)
	require.Len(t, data.Fixed, 1)
	assert.Equal(t, "CVE-1", data.Fixed[0].CVE)
	require.Len(t, data.Remaining, 1)
	assert.Equal(t, "CVE-2", data.Remaining[0].CVE)
}

func TestComposerAuditReportDataNilWhenNothingToReport(t *testing.T) {
	ca := &ComposerAudit{}
	assert.Nil(t, ca.ReportData(), "an addon with nothing to say must not add an empty section")
}

func TestUpdateHooksReportData(t *testing.T) {
	uh := &UpdateHooks{
		hooks: UpdateHooksPerSite{
			"default": {"foo_update_9001": drush.UpdateHook{Module: "foo", Description: "Add a field"}},
		},
	}

	assert.Equal(t, "update_hooks", uh.ReportKey())

	data, ok := uh.ReportData().(map[string]map[string]drush.UpdateHook)
	require.True(t, ok)
	require.Contains(t, data, "default")
	assert.Equal(t, "foo", data["default"]["foo_update_9001"].Module)
}

// ReportData must hand back a copy: the live map is mutex-guarded state that keeps being written
// while sites update concurrently, and the caller cannot know that.
func TestUpdateHooksReportDataCopiesTheLiveMap(t *testing.T) {
	uh := &UpdateHooks{
		hooks: UpdateHooksPerSite{
			"default": {"foo_update_9001": drush.UpdateHook{Module: "foo"}},
		},
	}

	data := uh.ReportData().(map[string]map[string]drush.UpdateHook)
	data["default"]["injected"] = drush.UpdateHook{Module: "bar"}

	assert.NotContains(t, uh.hooks["default"], "injected")
}

func TestUpdateHooksReportDataNilWhenEmpty(t *testing.T) {
	assert.Nil(t, (&UpdateHooks{hooks: UpdateHooksPerSite{}}).ReportData())
}

func TestUnsupportedModulesReportDataIsSortedByName(t *testing.T) {
	um := &UnsupportedModules{
		modules: map[string]drush.UnsupportedModule{
			"zebra": {Name: "zebra"},
			"alpha": {Name: "alpha"},
			"mango": {Name: "mango"},
		},
	}

	assert.Equal(t, "unsupported_modules", um.ReportKey())

	data, ok := um.ReportData().([]drush.UnsupportedModule)
	require.True(t, ok)
	require.Len(t, data, 3)
	// Deterministic ordering is what lets a consumer diff two reports directly.
	assert.Equal(t, []string{"alpha", "mango", "zebra"}, []string{data[0].Name, data[1].Name, data[2].Name})
}

func TestUnsupportedModulesReportDataNilWhenEmpty(t *testing.T) {
	assert.Nil(t, (&UnsupportedModules{modules: map[string]drush.UnsupportedModule{}}).ReportData())
}

func TestComposerPatchesReportData(t *testing.T) {
	h := &ComposerPatches1{
		patchUpdates: PatchUpdates{
			Removed:   []RemovedPatch{{Package: "drupal/foo", Reason: "fixed upstream"}},
			Conflicts: []ConflictPatch{{Package: "drupal/bar", NewVersion: "2.0.0"}},
		},
	}

	assert.Equal(t, "composer_patches", h.ReportKey())

	data, ok := h.ReportData().(Patches)
	require.True(t, ok)
	require.Len(t, data.Removed, 1)
	assert.Equal(t, "fixed upstream", data.Removed[0].Reason)
	require.Len(t, data.Conflicts, 1)
	assert.Equal(t, "drupal/bar", data.Conflicts[0].Package)
	assert.Empty(t, data.Updated)
}

func TestComposerPatchesReportDataNilWhenNoPatchChanges(t *testing.T) {
	assert.Nil(t, (&ComposerPatches1{}).ReportData())
}

// composer_diff deliberately contributes nothing: the same information is already in the
// report's top-level packages field, structured, straight from composer update.
func TestComposerDiffDoesNotReport(t *testing.T) {
	var addon any = &ComposerDiff{}
	_, isReporter := addon.(report.Reporter)
	assert.False(t, isReporter)
}
