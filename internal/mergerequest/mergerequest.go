// Package mergerequest renders a run's merge request from its report. It reads the report the
// way any consumer does, through its JSON, so the description cannot say anything the report
// does not publish.
package mergerequest

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"text/template"

	"github.com/drupdater/drupdater/internal/report"
)

// MaxDescriptionBytes is GitHub's cap on a pull request body, the tighter of the two platforms.
const MaxDescriptionBytes = 65536

// Title names the merge request after the run's mode and start date.
func Title(rep report.Report) string {
	if rep.Mode == report.ModeSecurity {
		return rep.StartedAt.Format("2006-01-02") + ": Drupal Security Updates"
	}
	return rep.StartedAt.Format("January 2006") + ": Drupal Maintenance Updates"
}

// Description renders the addon sections rep carries. Over MaxDescriptionBytes it drops the
// dependency links, the one part of the description that grows by more than a row per package.
func Description(rep report.Report) (string, error) {
	return DescriptionWith(rep, nil)
}

// DescriptionWith is Description plus the sections of addons outside drupdater: templates holds
// each one's text/template by its report key, rendered after the builtin sections in key order
// against that key's report section. A key the report has no section for renders nothing.
func DescriptionWith(rep report.Report, templates map[string]string) (string, error) {
	tmpl, err := parsedTemplates()
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %w", err)
	}
	if len(templates) > 0 {
		if tmpl, err = withAddonTemplates(tmpl, templates); err != nil {
			return "", err
		}
	}
	return describe(tmpl, rep, slices.Sorted(maps.Keys(templates)))
}

func describe(tmpl *template.Template, rep report.Report, addonKeys []string) (string, error) {
	s, published, err := readSections(rep)
	if err != nil {
		return "", err
	}
	extra := addonSections(published, addonKeys)

	description, err := render(tmpl, s, extra, true)
	if err != nil {
		return "", err
	}
	if len(description) <= MaxDescriptionBytes {
		return description, nil
	}
	return render(tmpl, s, extra, false)
}

func addonTemplateName(key string) string {
	return "addon:" + key
}

// withAddonTemplates clones tmpl, so the parsed builtin set stays the same for every run.
func withAddonTemplates(tmpl *template.Template, templates map[string]string) (*template.Template, error) {
	tmpl, err := tmpl.Clone()
	if err != nil {
		return nil, fmt.Errorf("failed to copy the templates: %w", err)
	}
	for key, text := range templates {
		if _, err := tmpl.New(addonTemplateName(key)).Parse(text); err != nil {
			return nil, fmt.Errorf("failed to parse the description template of addon %s: %w", key, err)
		}
	}
	return tmpl, nil
}

// addonSections reads each section from the report's JSON, so a template sees the published names.
func addonSections(published map[string]any, keys []string) []section {
	var out []section
	for _, key := range keys {
		if data, ok := published[key]; ok {
			out = append(out, section{addonTemplateName(key), data})
		}
	}
	return out
}

// sections is this package's reading of the report's addons object: only what it renders.
type sections struct {
	AllowPlugins *struct {
		Added []string `json:"added"`
	} `json:"composer_allow_plugins"`
	Patches     *patches                         `json:"composer_patches"`
	Diff        []dependencyChange               `json:"composer_diff"`
	UpdateHooks map[string]map[string]updateHook `json:"update_hooks"`
	Audit       *audit                           `json:"composer_audit"`
	Unsupported []unsupportedModule              `json:"unsupported_modules"`
}

type patches struct {
	Removed []struct {
		Package          string `json:"package"`
		PatchDescription string `json:"patch_description"`
		PatchPath        string `json:"patch_path"`
		Reason           string `json:"reason"`
	} `json:"removed"`
	Updated []struct {
		Package           string `json:"package"`
		PatchDescription  string `json:"patch_description"`
		PreviousPatchPath string `json:"previous_patch_path"`
		NewPatchPath      string `json:"new_patch_path"`
	} `json:"updated"`
	Conflicts []struct {
		Package          string `json:"package"`
		FixedVersion     string `json:"fixed_version"`
		PatchPath        string `json:"patch_path"`
		PatchDescription string `json:"patch_description"`
		NewVersion       string `json:"new_version"`
	} `json:"conflicts"`
}

type dependencyChange struct {
	Package    string `json:"package"`
	Operation  string `json:"operation"`
	From       string `json:"from"`
	To         string `json:"to"`
	Dev        bool   `json:"dev"`
	CompareURL string `json:"compare_url"`
	ProjectURL string `json:"project_url"`
}

type updateHook struct {
	Description string `json:"description"`
}

type advisory struct {
	CVE         string `json:"cve"`
	Title       string `json:"title"`
	Severity    string `json:"severity"`
	PackageName string `json:"packageName"`
}

type audit struct {
	Fixed     []advisory `json:"fixed"`
	Remaining []advisory `json:"remaining"`
	Abandoned []struct {
		PackageName string `json:"packageName"`
		Replacement string `json:"replacement"`
	} `json:"abandoned"`
}

type unsupportedModule struct {
	Name               string `json:"name"`
	InstalledVersion   string `json:"installed_version"`
	RecommendedVersion string `json:"recommended_version"`
}

// readSections returns the builtin sections and, for addons outside drupdater, every section as
// generic JSON.
func readSections(rep report.Report) (sections, map[string]any, error) {
	encoded, err := json.Marshal(rep.Addons)
	if err != nil {
		return sections{}, nil, fmt.Errorf("failed to encode the report's addon sections: %w", err)
	}
	var s sections
	if err := json.Unmarshal(encoded, &s); err != nil {
		return sections{}, nil, fmt.Errorf("failed to read the report's addon sections: %w", err)
	}
	var published map[string]any
	// Cannot fail where the typed read above succeeded: the same document, into a looser type.
	_ = json.Unmarshal(encoded, &published)
	return s, published, nil
}

// endOfLifeEntry is one row of the table that joins unsupported modules and abandoned packages:
// one finding to a reviewer, so one list.
type endOfLifeEntry struct {
	Name             string
	Status           string
	InstalledVersion string
	Recommendation   string
}

// endOfLife is sorted by name, so the two sources interleave rather than read as two lists.
func (s sections) endOfLife() []endOfLifeEntry {
	var entries []endOfLifeEntry
	for _, module := range s.Unsupported {
		// drush reports "None" when there is no supported release — a status, not a version.
		recommendation := "Update to " + module.RecommendedVersion
		if module.RecommendedVersion == "" || module.RecommendedVersion == "None" {
			recommendation = "No supported release — replace it"
		}
		entries = append(entries, endOfLifeEntry{
			Name:             module.Name,
			Status:           "Unsupported module",
			InstalledVersion: module.InstalledVersion,
			Recommendation:   recommendation,
		})
	}
	if s.Audit != nil {
		for _, pkg := range s.Audit.Abandoned {
			recommendation := "No replacement suggested"
			if pkg.Replacement != "" {
				recommendation = "Replace with " + pkg.Replacement
			}
			entries = append(entries, endOfLifeEntry{Name: pkg.PackageName, Status: "Abandoned package", Recommendation: recommendation})
		}
	}
	slices.SortStableFunc(entries, func(a, b endOfLifeEntry) int { return strings.Compare(a.Name, b.Name) })
	return entries
}

type diffTable struct {
	Title   string
	Changes []dependencyChange
}

func (s sections) diffTables() []diffTable {
	var prod, dev []dependencyChange
	for _, change := range s.Diff {
		if change.Dev {
			dev = append(dev, change)
		} else {
			prod = append(prod, change)
		}
	}

	var tables []diffTable
	if len(prod) > 0 {
		tables = append(tables, diffTable{Title: "Prod Packages", Changes: prod})
	}
	if len(dev) > 0 {
		tables = append(tables, diffTable{Title: "Dev Packages", Changes: dev})
	}
	return tables
}

// section is one template and the data it renders, or nil data when the report has nothing for it.
type section struct {
	template string
	data     any
}

// sectionsInOrder fixes the description's order: what needs a decision first, logs last.
func (s sections) sectionsInOrder(links bool) []section {
	var out []section
	if s.AllowPlugins != nil && len(s.AllowPlugins.Added) > 0 {
		out = append(out, section{"composer_allow_plugins.go.tmpl", s.AllowPlugins.Added})
	}
	if s.Patches != nil {
		out = append(out, section{"composer_patches.go.tmpl", s.Patches})
	}
	if tables := s.diffTables(); len(tables) > 0 {
		out = append(out, section{"composer_diff.go.tmpl", struct {
			Tables []diffTable
			Links  bool
		}{tables, links}})
	}
	if s.Audit != nil && (len(s.Audit.Fixed) > 0 || len(s.Audit.Remaining) > 0) {
		out = append(out, section{"composer_audit.go.tmpl", s.Audit})
	}
	if entries := s.endOfLife(); len(entries) > 0 {
		out = append(out, section{"unsupported_modules.go.tmpl", entries})
	}
	if len(s.UpdateHooks) > 0 {
		out = append(out, section{"update_hooks.go.tmpl", s.UpdateHooks})
	}
	return out
}

func render(tmpl *template.Template, s sections, extra []section, links bool) (string, error) {
	var rendered []string
	for _, sec := range slices.Concat(s.sectionsInOrder(links), extra) {
		out, err := renderSection(tmpl, sec)
		if err != nil {
			return "", err
		}
		rendered = append(rendered, out)
	}

	return execute(tmpl, "description.go.tmpl", rendered)
}

// renderSection ends the section in exactly one newline, so sections are a blank line apart.
func renderSection(tmpl *template.Template, sec section) (string, error) {
	out, err := execute(tmpl, sec.template, sec.data)
	return strings.TrimRight(out, "\n") + "\n", err
}

func execute(tmpl *template.Template, name string, data any) (string, error) {
	var out bytes.Buffer
	if err := tmpl.ExecuteTemplate(&out, name, data); err != nil {
		return "", fmt.Errorf("failed to execute template %s: %w", name, err)
	}
	return out.String(), nil
}

//go:embed templates
var templates embed.FS

// cellReplacer stops a literal "|" or newline in a value from breaking a markdown table.
var cellReplacer = strings.NewReplacer("|", "\\|", "\n", " ", "\r", "")

var operations = map[string]string{
	"install":   "New",
	"remove":    "Removed",
	"upgrade":   "Upgraded",
	"downgrade": "Downgraded",
	"change":    "Changed",
}

// parsedTemplates parses the embedded templates once: the FS is compiled in, so the result is fixed.
var parsedTemplates = sync.OnceValues(func() (*template.Template, error) {
	return template.New("").Funcs(template.FuncMap{
		"cell": cellReplacer.Replace,
		"operation": func(op string) string {
			if label, ok := operations[op]; ok {
				return label
			}
			return cellReplacer.Replace(op)
		},
		"version": func(v string) string {
			if v == "" {
				return "-"
			}
			return cellReplacer.Replace(v)
		},
	}).ParseFS(templates, "templates/*.go.tmpl")
})
