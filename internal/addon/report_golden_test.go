package addon

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/drupdater/drupdater/internal"
	"github.com/drupdater/drupdater/internal/golden"
	"github.com/drupdater/drupdater/internal/report"
	"github.com/drupdater/drupdater/pkg/composer"
	"github.com/drupdater/drupdater/pkg/drush"
	"github.com/stretchr/testify/require"
)

// The --report document is a published contract: consumers key off these names, and renaming one
// is a SchemaVersion bump. Nothing else in the suite notices such a rename — struct tags are
// invisible to coverage, and mutation testing does not mutate them — so the whole document is
// pinned here, with every reporting addon contributing at once.
//
// The file this compares against is embedded into docs/reference/run-report.md, so the published
// example cannot drift from what the code emits either.
//
// Regenerate after a deliberate change with: go test ./internal/addon -update

// fixedTime is the instant every timestamp in the golden report is normalised to.
var fixedTime = time.Date(2026, 3, 14, 9, 30, 0, 0, time.UTC)

// reportingAddons builds one of every addon that contributes a section, populated so no field is
// left at its zero value — an omitempty field that is never set cannot be pinned.
func reportingAddons() []internal.Addon {
	beforeAudit := composer.Audit{
		Advisories: []composer.Advisory{
			{
				PackageName: "drupal/core", AdvisoryID: "PKSA-2026-0001", CVE: "CVE-2026-1111",
				Title: "Cross site scripting in the render pipeline", Link: "https://www.drupal.org/sa-core-2026-001",
				AffectedVersions: ">=10.3.0,<10.3.9", Severity: "critical", ReportedAt: "2026-02-01T00:00:00+00:00",
			},
			{
				PackageName: "drupal/token", AdvisoryID: "PKSA-2026-0002", CVE: "CVE-2026-2222",
				Title: "Access bypass", Link: "https://www.drupal.org/sa-contrib-2026-002",
				AffectedVersions: "<1.15.0", Severity: "moderate", ReportedAt: "2026-02-10T00:00:00+00:00",
			},
		},
	}
	afterAudit := composer.Audit{
		// drupal/token stays: its fix needs a major bump the update did not take.
		Advisories: beforeAudit.Advisories[1:],
		Abandoned: []composer.AbandonedPackage{
			{PackageName: "swiftmailer/swiftmailer", Replacement: "symfony/mailer"},
			{PackageName: "patchwork/jsqueeze"},
		},
	}

	return []internal.Addon{
		&ComposerAudit{beforeAudit: beforeAudit, afterAudit: afterAudit},
		&UpdateHooks{hooks: UpdateHooksPerSite{
			"default": {"token_update_8002": {Module: "token", UpdateID: 8002, Description: "Rebuild the token cache", Type: "hook_update_n"}},
			"second":  {"node_post_update_index": {Module: "node", UpdateID: "index", Description: "Reindex nodes", Type: "post_update"}},
		}},
		&UnsupportedModules{modules: map[string]drush.UnsupportedModule{
			"legacy_feature": {Name: "legacy_feature", InstalledVersion: "1.2.0", RecommendedVersion: "None"},
			"old_widget":     {Name: "old_widget", InstalledVersion: "2.0.0", RecommendedVersion: "3.1.0"},
		}},
		&ComposerPatches1{patchUpdates: PatchUpdates{
			Removed: []RemovedPatch{{
				Package: "drupal/core", PatchDescription: "Fix the thing",
				PatchPath: "https://www.drupal.org/files/issues/3001-12.patch", Reason: "fixed upstream in 10.4.0",
			}},
			Updated: []UpdatedPatch{{
				Package: "drupal/paragraphs", PatchDescription: "Support the other thing",
				PreviousPatchPath: "https://www.drupal.org/files/issues/3002-4.patch",
				NewPatchPath:      "https://www.drupal.org/files/issues/3002-9.patch",
			}},
			Conflicts: []ConflictPatch{{
				Package: "drupal/webform", FixedVersion: "6.2.7",
				PatchPath:        "https://www.drupal.org/files/issues/3003-2.patch",
				PatchDescription: "Adjust the form", NewVersion: "6.3.0",
			}},
		}},
		&CodeBeautifier{fixedFiles: []string{"web/modules/custom/acme/acme.module", "web/modules/custom/acme/src/Controller/AcmeController.php"}, fixable: 3},
		&DeprecationsRemover{fixes: []DeprecationFix{{
			File:           "web/modules/custom/acme/src/Plugin/Block/AcmeBlock.php",
			AppliedRectors: []string{"Drupal\\Rector\\Rector\\Deprecation\\DrupalSetMessageRector"},
		}}},
		&TranslationsUpdater{results: map[string]TranslationResult{
			"default": {Path: "translations", Updated: true},
			"second":  {Skipped: "locale_deploy not enabled"},
		}},
	}
}

// normaliseTimes replaces everything a clock produced, so the document is byte-stable while every
// field name it carries stays pinned.
func normaliseTimes(rep *report.Report) {
	rep.StartedAt = fixedTime
	rep.FinishedAt = fixedTime.Add(14*time.Minute + 31*time.Second)
	rep.DurationSeconds = rep.FinishedAt.Sub(rep.StartedAt).Seconds()
	for i := range rep.Phases {
		rep.Phases[i].StartedAt = fixedTime.Add(time.Duration(i) * time.Minute)
		rep.Phases[i].DurationSeconds = float64(i+1) * 1.5
	}
}

func TestRunReportMatchesItsGoldenFile(t *testing.T) {
	// The real recorder rather than a hand-built struct, so the golden also pins how AddAddons
	// keys the sections and what Finish fills in.
	rec := report.NewRecorder("v1.4.0", report.ModeSecurity, false,
		"https://oauth2:token@github.com/org/site.git", "main", []string{"default", "second"})

	rec.SetToolVersions(report.ToolVersions{ComposerVersion: "2.10.2", PHPVersion: "8.3.14"})
	require.NoError(t, rec.Run("preflight", func() error { return nil }))
	require.NoError(t, rec.Run("update shared code", func() error { return nil }))
	require.NoError(t, rec.Run("site update", func() error { return nil }))
	rec.SetUpdateBranch("update-3f81a2c")
	rec.SetPackages([]report.PackageChange{
		{Action: "Upgrade", Package: "drupal/core", From: "10.3.8", To: "10.3.9"},
		{Action: "Install", Package: "drupal/redirect", To: "1.10.0"},
		{Action: "Remove", Package: "drupal/legacy_feature", From: "1.2.0"},
	})
	rec.SetMergeRequestContent("Drupal Security Update", "## 🔒 **Security Report**\n")
	rec.SetMergeRequest("https://github.com/org/site/pull/42")
	rec.SetAutoMerge(nil)
	rec.AddAddons(reportingAddons())

	rep := rec.Finish()
	normaliseTimes(&rep)

	encoded, err := json.MarshalIndent(rep, "", "  ")
	require.NoError(t, err)

	golden.Assert(t, "testdata/run_report.json", string(encoded)+"\n")
}

// The credential in the repository URL above must not reach the document, and a golden file is a
// poor place to notice it: a leak would simply be baked in on the next -update.
func TestRunReportGoldenCarriesNoCredential(t *testing.T) {
	rec := report.NewRecorder("v1.4.0", report.ModeSecurity, false,
		"https://oauth2:token@github.com/org/site.git", "main", []string{"default"})
	rec.AddAddons(reportingAddons())

	encoded, err := json.Marshal(rec.Finish())
	require.NoError(t, err)

	require.NotContains(t, string(encoded), "oauth2:token")
}
