package addon

import (
	"github.com/drupdater/drupdater/pkg/rector"
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

func TestComposerAuditReportDataOnEitherHalfAlone(t *testing.T) {
	advisory := composer.Advisory{CVE: "CVE-3", PackageName: "drupal/baz"}

	t.Run("only fixed advisories", func(t *testing.T) {
		// The update closed the advisory: worth reporting on its own, since it is the evidence
		// the update was security-relevant.
		ca := &ComposerAudit{
			beforeAudit: composer.Audit{Advisories: []composer.Advisory{advisory}},
			afterAudit:  composer.Audit{},
		}
		data, ok := ca.ReportData().(SecurityAdvisories)
		require.True(t, ok, "a fixed advisory alone must still produce a section")
		require.Len(t, data.Fixed, 1)
		assert.Equal(t, "CVE-3", data.Fixed[0].CVE)
		assert.Empty(t, data.Remaining)
	})

	t.Run("only remaining advisories", func(t *testing.T) {
		// Nothing was fixed but something is still open: reporting this is the whole point of
		// the audit, and suppressing it would hide a live vulnerability from the reviewer.
		ca := &ComposerAudit{
			beforeAudit: composer.Audit{Advisories: []composer.Advisory{advisory}},
			afterAudit:  composer.Audit{Advisories: []composer.Advisory{advisory}},
		}
		data, ok := ca.ReportData().(SecurityAdvisories)
		require.True(t, ok, "a remaining advisory alone must still produce a section")
		assert.Empty(t, data.Fixed)
		require.Len(t, data.Remaining, 1)
		assert.Equal(t, "CVE-3", data.Remaining[0].CVE)
	})
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

func TestCodeBeautifierReportData(t *testing.T) {
	cb := &CodeBeautifier{}

	t.Run("nothing fixed reports nothing", func(t *testing.T) {
		assert.Equal(t, "code_beautifier", cb.ReportKey())
		assert.Nil(t, cb.ReportData())
	})

	t.Run("reports the files it committed and what was fixable", func(t *testing.T) {
		cb := &CodeBeautifier{
			fixedFiles: []string{"web/modules/custom/a/a.module", "web/modules/custom/b/b.module"},
			fixable:    7,
		}
		assert.Equal(t, CodingStyleFixes{
			Files:   []string{"web/modules/custom/a/a.module", "web/modules/custom/b/b.module"},
			Fixable: 7,
		}, cb.ReportData())
	})
}

func TestDeprecationsRemoverReportData(t *testing.T) {
	t.Run("nothing rewritten reports nothing", func(t *testing.T) {
		dr := &DeprecationsRemover{}
		assert.Equal(t, "deprecations_remover", dr.ReportKey())
		assert.Nil(t, dr.ReportData())
	})

	t.Run("records which rules fired on which file, sorted", func(t *testing.T) {
		dr := &DeprecationsRemover{}
		dr.recordFixes(rector.ReturnOutput{
			Totals:       rector.ReturnOutputTotals{ChangedFiles: 2},
			ChangedFiles: []string{"web/modules/custom/z/z.php", "web/modules/custom/a/a.php"},
			FileDiffs: []rector.ReturnOutputFillDiff{
				{File: "web/modules/custom/a/a.php", AppliedRectors: []string{"SecondRector", "FirstRector"}},
				{File: "web/modules/custom/z/z.php", AppliedRectors: []string{"OtherRector"}},
			},
		})

		// Files and rule names are both sorted, so two runs over unchanged code produce
		// byte-identical sections and a consumer can diff reports directly.
		assert.Equal(t, []DeprecationFix{
			{File: "web/modules/custom/a/a.php", AppliedRectors: []string{"FirstRector", "SecondRector"}},
			{File: "web/modules/custom/z/z.php", AppliedRectors: []string{"OtherRector"}},
		}, dr.ReportData())
	})

	t.Run("a changed file with no recorded rules still reports", func(t *testing.T) {
		dr := &DeprecationsRemover{}
		dr.recordFixes(rector.ReturnOutput{
			Totals:       rector.ReturnOutputTotals{ChangedFiles: 1},
			ChangedFiles: []string{"a.php"},
		})
		assert.Equal(t, []DeprecationFix{{File: "a.php"}}, dr.ReportData())
	})
}

func TestTranslationsUpdaterReportData(t *testing.T) {
	t.Run("never ran reports nothing", func(t *testing.T) {
		tu := &TranslationsUpdater{}
		assert.Equal(t, "translations_updater", tu.ReportKey())
		assert.Nil(t, tu.ReportData())
	})

	t.Run("distinguishes updated, unchanged and skipped, per site", func(t *testing.T) {
		// The skipped case is the one that matters: it used to be visible only in the log,
		// so a report that omitted the site could not tell it apart from "nothing to do".
		tu := &TranslationsUpdater{}
		tu.record("one", TranslationResult{Path: "translations", Updated: true})
		tu.record("two", TranslationResult{Path: "translations"})
		tu.record("three", TranslationResult{Skipped: "locale_deploy not enabled"})

		assert.Equal(t, map[string]TranslationResult{
			"one":   {Path: "translations", Updated: true},
			"two":   {Path: "translations"},
			"three": {Skipped: "locale_deploy not enabled"},
		}, tu.ReportData())
	})

	t.Run("the returned map is a copy of guarded state", func(t *testing.T) {
		tu := &TranslationsUpdater{}
		tu.record("default", TranslationResult{Updated: true})
		out := tu.ReportData().(map[string]TranslationResult)
		out["default"] = TranslationResult{}
		assert.True(t, tu.ReportData().(map[string]TranslationResult)["default"].Updated)
	})
}
