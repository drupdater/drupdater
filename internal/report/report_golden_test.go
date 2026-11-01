package report_test

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/drupdater/drupdater/internal/golden"
	"github.com/drupdater/drupdater/internal/report"
)

// The --report document is a published contract: consumers key off these names, and renaming one
// is a SchemaVersion bump. Nothing else in the suite notices such a rename -- struct tags are
// invisible to coverage, and mutation testing does not mutate them -- so the whole document is
// pinned here. Its addon sections are the ones the agent builds for agent/protocol/report's
// request, which the agent's own suite pins, so a rename on either side fails a golden.
//
// The file this compares against is embedded into docs/reference/run-report.md, so the published
// example cannot drift from what the code emits either.
//
// Regenerate after a deliberate change with: go test ./internal/report -update

// fixedTime is the instant every timestamp in the golden report is normalised to.
var fixedTime = time.Date(2026, 3, 14, 9, 30, 0, 0, time.UTC)

// agentSections are the sections the agent's report fixture answers with, every addon that
// reports anything populated.
func agentSections(t *testing.T) map[string]json.RawMessage {
	t.Helper()
	raw, err := os.ReadFile("../../agent/protocol/report/response.json")
	require.NoError(t, err)
	var reply struct {
		Result struct {
			Sections map[string]json.RawMessage `json:"sections"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(raw, &reply))
	require.NotEmpty(t, reply.Result.Sections)
	return reply.Result.Sections
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
	// The real recorder rather than a hand-built struct, so the golden also pins how the sections
	// are keyed and what Finish fills in.
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
	for name, section := range agentSections(t) {
		rec.AddSection(name, section)
	}

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
	for name, section := range agentSections(t) {
		rec.AddSection(name, section)
	}

	encoded, err := json.Marshal(rec.Finish())
	require.NoError(t, err)

	require.NotContains(t, string(encoded), "oauth2:token")
}
