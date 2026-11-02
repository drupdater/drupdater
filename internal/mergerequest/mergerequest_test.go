package mergerequest_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"text/template"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/drupdater/drupdater/internal/golden"
	"github.com/drupdater/drupdater/internal/mergerequest"
	"github.com/drupdater/drupdater/internal/report"
)

// publishedExample is the report docs/reference/run-report.md shows, every section populated.
func publishedExample(t *testing.T) report.Report {
	t.Helper()
	encoded, err := os.ReadFile("../report/testdata/run_report.json")
	require.NoError(t, err)
	var rep report.Report
	require.NoError(t, json.Unmarshal(encoded, &rep))
	return rep
}

func withSections(sections map[string]any) report.Report {
	return report.Report{Addons: sections}
}

// dependencyChange is one row of composer_diff's section, as the run report publishes it.
type dependencyChange struct {
	Package    string `json:"package"`
	Operation  string `json:"operation"`
	From       string `json:"from,omitempty"`
	To         string `json:"to,omitempty"`
	Dev        bool   `json:"dev"`
	Direct     bool   `json:"direct"`
	CompareURL string `json:"compare_url,omitempty"`
	ProjectURL string `json:"project_url,omitempty"`
}

// The description rendered from the published example report. The section goldens are the
// examples on the addon reference pages.
func TestDescriptionOfThePublishedExample(t *testing.T) {
	rep := publishedExample(t)

	description, err := mergerequest.Description(rep)
	require.NoError(t, err)
	golden.Assert(t, "testdata/description.md", description)

	names, sections, err := mergerequest.RenderedSections(rep)
	require.NoError(t, err)
	for i, name := range names {
		t.Run(name, func(t *testing.T) {
			assert.Contains(t, description, sections[i], "the description is made of its sections")
			golden.Assert(t, "testdata/"+strings.TrimSuffix(name, ".go.tmpl")+".md", sections[i])
		})
	}
}

func TestEveryTemplateIsReachedByThePublishedExample(t *testing.T) {
	reached, _, err := mergerequest.RenderedSections(publishedExample(t))
	require.NoError(t, err)

	assert.Equal(t, []string{
		"composer_allow_plugins.go.tmpl",
		"composer_patches.go.tmpl",
		"composer_diff.go.tmpl",
		"composer_audit.go.tmpl",
		"unsupported_modules.go.tmpl",
		"update_hooks.go.tmpl",
	}, reached)
}

func TestTitle(t *testing.T) {
	started := time.Date(2026, 7, 25, 9, 0, 0, 0, time.UTC)

	assert.Equal(t, "July 2026: Drupal Maintenance Updates", mergerequest.Title(report.Report{Mode: report.ModeNormal, StartedAt: started}))
	assert.Equal(t, "2026-07-25: Drupal Security Updates", mergerequest.Title(report.Report{Mode: report.ModeSecurity, StartedAt: started}))
}

func TestAReportWithoutSectionsDescribesOnlyTheMergeRequest(t *testing.T) {
	description, err := mergerequest.Description(report.Report{})

	require.NoError(t, err)
	assert.Equal(t, "This automated merge request by [Drupdater](https://github.com/drupdater/drupdater) includes "+
		"updates for your Drupal site. Please review the changes carefully to ensure compatibility and stability "+
		"before merging.\n", description)
}

func TestAnUnreadableSectionFailsTheDescription(t *testing.T) {
	_, err := mergerequest.Description(withSections(map[string]any{"composer_audit": "not an object"}))

	require.ErrorContains(t, err, "failed to read the report's addon sections")
	var typeErr *json.UnmarshalTypeError
	require.ErrorAs(t, err, &typeErr)
}

func TestAnUnencodableSectionFailsTheDescription(t *testing.T) {
	_, err := mergerequest.Description(withSections(map[string]any{"composer_audit": func() {}}))

	require.ErrorContains(t, err, "failed to encode the report's addon sections")
	var typeErr *json.UnsupportedTypeError
	require.ErrorAs(t, err, &typeErr)
}

func TestAFailingSectionTemplateFailsTheDescription(t *testing.T) {
	tmpl := template.Must(template.New("").Parse(`{{ define "composer_allow_plugins.go.tmpl" }}{{ .Missing }}{{ end }}`))

	description, err := mergerequest.DescribeWith(tmpl, withSections(map[string]any{
		"composer_allow_plugins": map[string]any{"added": []string{"acme/plugin"}},
	}))

	require.ErrorContains(t, err, "failed to execute template composer_allow_plugins.go.tmpl")
	var execErr template.ExecError
	require.ErrorAs(t, err, &execErr)
	assert.Empty(t, description)
}

func TestAPluginListRendersOnlyWhenSomethingWasAdded(t *testing.T) {
	empty, err := mergerequest.Description(withSections(map[string]any{
		"composer_allow_plugins": map[string]any{"added": []string{}},
	}))
	require.NoError(t, err)
	assert.NotContains(t, empty, "New Composer plugins")

	one, err := mergerequest.Description(withSections(map[string]any{
		"composer_allow_plugins": map[string]any{"added": []string{"acme/plugin"}},
	}))
	require.NoError(t, err)
	assert.Contains(t, one, "## 🔌 New Composer plugins")
	assert.Contains(t, one, "composer config allow-plugins.acme/plugin true\n")
}

// One site needs no per-site heading; the published example covers several.
func TestASingleSiteListsItsHooksWithoutASiteHeading(t *testing.T) {
	description, err := mergerequest.Description(withSections(map[string]any{
		"update_hooks": map[string]any{"default": map[string]any{"token_update_8002": map[string]any{"description": "Rebuild"}}},
	}))

	require.NoError(t, err)
	assert.Contains(t, description, "| token_update_8002 | Rebuild |\n")
	assert.NotContains(t, description, "### Site:")
}

// Sections the description does not render are read past, not rejected: the report may grow.
func TestSectionsWithoutATemplateAreIgnored(t *testing.T) {
	description, err := mergerequest.Description(withSections(map[string]any{
		"code_beautifier": map[string]any{"files": []string{"a.php"}, "fixable": 1},
	}))

	require.NoError(t, err)
	assert.NotContains(t, description, "##")
}

func TestAdvisoryCellsCannotBreakTheTable(t *testing.T) {
	description, err := mergerequest.Description(withSections(map[string]any{"composer_audit": map[string]any{
		"remaining": []map[string]any{{"packageName": "drupal/foo", "cve": "CVE-1", "title": "XSS via a|b\nsecond line"}},
	}}))

	require.NoError(t, err)
	assert.Contains(t, description, "XSS via a\\|b second line")
	assert.NotContains(t, description, "a|b")
	assert.Contains(t, description, "There are still 1 unresolved issue. Please investigate manually.")
}

// A run that closed every advisory still has something to say.
func TestASecurityRunThatFixedEverythingSaysSo(t *testing.T) {
	description, err := mergerequest.Description(withSections(map[string]any{"composer_audit": map[string]any{
		"fixed": []map[string]any{{"packageName": "drupal/core", "cve": "CVE-1", "title": "Closed"}},
	}}))

	require.NoError(t, err)
	assert.Contains(t, description, "| ✅     | CVE-1 | Closed |")
	assert.Contains(t, description, "All security issues have been resolved.")
}

// Abandoned packages belong to the end-of-life table; the security section shows advisories only.
func TestAbandonedPackagesAloneGetTheEndOfLifeTableNotTheSecuritySection(t *testing.T) {
	description, err := mergerequest.Description(withSections(map[string]any{"composer_audit": map[string]any{
		"abandoned": []map[string]any{{"packageName": "patchwork/jsqueeze", "replacement": ""}},
	}}))

	require.NoError(t, err)
	assert.NotContains(t, description, "Security Report")
	assert.Contains(t, description, "| patchwork/jsqueeze | Abandoned package | — | No replacement suggested |")
}

// One list sorted by name, the two sources interleaved, each row phrased for its source. drush's
// literal "None" is a status, not a version to update to.
func TestEndOfLifeTableJoinsBothSourcesByName(t *testing.T) {
	description, err := mergerequest.Description(withSections(map[string]any{
		"unsupported_modules": []map[string]any{
			{"name": "upgrade", "installed_version": "2.0.0", "recommended_version": "3.0.0"},
			{"name": "none", "installed_version": "1.0.0", "recommended_version": "None"},
			{"name": "empty"},
		},
		"composer_audit": map[string]any{"abandoned": []map[string]any{
			{"packageName": "b/orphan", "replacement": ""},
			{"packageName": "a/replaced", "replacement": "a/next"},
		}},
	}))

	require.NoError(t, err)
	assert.Contains(t, description, "| Name | Status | Installed version | Recommendation |\n"+
		"| ---- | ------ | ----------------- | -------------- |\n"+
		"| a/replaced | Abandoned package | — | Replace with a/next |\n"+
		"| b/orphan | Abandoned package | — | No replacement suggested |\n"+
		"| empty | Unsupported module | — | No supported release — replace it |\n"+
		"| none | Unsupported module | 1.0.0 | No supported release — replace it |\n"+
		"| upgrade | Unsupported module | 2.0.0 | Update to 3.0.0 |\n")
}

// Each kind of patch change alone produces the section.
func TestAnyPatchChangeRendersThePatchSection(t *testing.T) {
	for name, patches := range map[string]map[string]any{
		"only removals":  {"removed": []map[string]any{{"package": "drupal/core", "patch_path": "p.patch", "reason": "fixed"}}, "updated": []any{}, "conflicts": []any{}},
		"only updates":   {"removed": []any{}, "updated": []map[string]any{{"package": "drupal/core", "previous_patch_path": "old.patch", "new_patch_path": "new.patch"}}, "conflicts": []any{}},
		"only conflicts": {"removed": []any{}, "updated": []any{}, "conflicts": []map[string]any{{"package": "drupal/core", "fixed_version": "1.0", "new_version": "1.1"}}},
	} {
		t.Run(name, func(t *testing.T) {
			description, err := mergerequest.Description(withSections(map[string]any{"composer_patches": patches}))

			require.NoError(t, err)
			assert.Contains(t, description, "## 🩹 Patch updates")
			assert.Contains(t, description, "drupal/core")
		})
	}
}

func TestDependencyTablesSplitProdFromDev(t *testing.T) {
	description, err := mergerequest.Description(withSections(map[string]any{"composer_diff": []dependencyChange{
		{Package: "drupal/core", Operation: "change", From: "dev-main abc", To: "dev-main def"},
		{Package: "acme/tool", Operation: "downgrade", From: "2.0", To: "1.9", Dev: true},
		{Package: "acme/odd", Operation: "renamed|", From: "1.0", To: "1.1"},
	}}))

	require.NoError(t, err)
	assert.Contains(t, description, "| Prod Packages | Operation | Base | Target | Link |\n")
	assert.Contains(t, description, "| drupal/core | Changed | dev-main abc | dev-main def |  |\n")
	assert.Contains(t, description, "| acme/odd | renamed\\| | 1.0 | 1.1 |  |\n", "an unknown operation is shown as is")
	assert.Contains(t, description, "| Dev Packages | Operation | Base | Target | Link |\n| --- | --- | --- | --- | --- |\n"+
		"| acme/tool | Downgraded | 2.0 | 1.9 |  |\n")
	assert.Less(t, strings.Index(description, "Prod Packages"), strings.Index(description, "Dev Packages"))
}

func TestOnlyDevChangesRenderOnlyTheDevTable(t *testing.T) {
	description, err := mergerequest.Description(withSections(map[string]any{"composer_diff": []dependencyChange{
		{Package: "acme/tool", Operation: "install", To: "1.0", Dev: true},
	}}))

	require.NoError(t, err)
	assert.NotContains(t, description, "Prod Packages")
	assert.Contains(t, description, "| acme/tool | New | - | 1.0 |  |\n")
}

// GitHub rejects a longer body outright, which would fail the run after the branch was pushed.
func TestAnOversizedDescriptionDropsTheDependencyLinks(t *testing.T) {
	url := "https://github.com/acme/" + strings.Repeat("x", 300)
	changes := make([]dependencyChange, 0, 300)
	for i := range 300 {
		changes = append(changes, dependencyChange{
			Package: "acme/p" + strings.Repeat("y", i%10), Operation: "upgrade", From: "1.0", To: "1.1",
			CompareURL: url, ProjectURL: url,
		})
	}

	description, err := mergerequest.Description(withSections(map[string]any{"composer_diff": changes}))

	require.NoError(t, err)
	assert.LessOrEqual(t, len(description), mergerequest.MaxDescriptionBytes)
	assert.NotContains(t, description, "[Compare]")
	assert.NotContains(t, description, "| Link |")
	assert.Contains(t, description, "| acme/p | Upgraded | 1.0 | 1.1 |\n")
}

// The limit is inclusive: a description of exactly MaxDescriptionBytes is accepted as it is.
func TestADescriptionAtExactlyTheLimitKeepsItsLinks(t *testing.T) {
	withName := func(name string) report.Report {
		return withSections(map[string]any{"composer_diff": []dependencyChange{
			{Package: name, Operation: "upgrade", From: "1.0", To: "1.1", CompareURL: "https://c"},
		}})
	}
	base, err := mergerequest.Description(withName("a"))
	require.NoError(t, err)

	description, err := mergerequest.Description(withName(strings.Repeat("a", 1+mergerequest.MaxDescriptionBytes-len(base))))

	require.NoError(t, err)
	assert.Len(t, description, mergerequest.MaxDescriptionBytes)
	assert.Contains(t, description, "[Compare](https://c)")
}

func TestADescriptionWithinTheLimitKeepsItsLinks(t *testing.T) {
	description, err := mergerequest.Description(withSections(map[string]any{"composer_diff": []dependencyChange{
		{Package: "drupal/core", Operation: "upgrade", From: "1.0", To: "1.1", CompareURL: "https://c", ProjectURL: "https://p"},
	}}))

	require.NoError(t, err)
	assert.Contains(t, description, "| [drupal/core](https://p) | Upgraded | 1.0 | 1.1 | [Compare](https://c) |\n")
}
