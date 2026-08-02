package addon

import (
	"maps"
	"slices"
	"strings"

	"github.com/drupdater/drupdater/pkg/composer"
	"github.com/drupdater/drupdater/pkg/drush"
)

// This file holds every addon's contribution to the machine-readable run report (--report).
//
// Together these methods are the report's addons schema, a published contract. Scattered across
// eight files it is easy to rename a field without noticing a consumer depends on it.
//
// Each addon satisfies report.Reporter structurally, so none of them imports the report package.
// The keys match .drupdater.yaml's addon names, so a report reads the way the project is
// configured.
//
// Two addons deliberately contribute nothing:
//
//   - composer_diff renders a markdown table of what the top-level "packages" field already
//     carries in structured form.
//   - composer_normalizer only reorders composer.json, which the diff shows.
//
// Everything else reports, even addons whose work is "only" a code change: most log and swallow
// their own failures, so one that silently did nothing is otherwise indistinguishable from one
// with nothing to do.

// --- composer_audit ---

// SecurityAdvisories is the composer_audit section: which advisories the update resolved and
// which are still open. Remaining is the more actionable half — a fleet-wide exposure view is
// built from it.
//
// Abandoned is not an advisory but the same shape of finding as an unsupported module, on the
// non-Drupal packages unsupported_modules cannot see.
type SecurityAdvisories struct {
	Fixed     []composer.Advisory         `json:"fixed"`
	Remaining []composer.Advisory         `json:"remaining"`
	Abandoned []composer.AbandonedPackage `json:"abandoned"`
}

// ReportKey implements report.Reporter.
func (ca *ComposerAudit) ReportKey() string { return "composer_audit" }

// ReportData implements report.Reporter. The abandoned packages are already sorted by name
// when composer's output is parsed, so this section stays byte-stable across runs.
func (ca *ComposerAudit) ReportData() any {
	fixed := ca.GetFixedAdvisories()
	remaining := ca.afterAudit.Advisories
	abandoned := ca.GetAbandonedPackages()
	if len(fixed) == 0 && len(remaining) == 0 && len(abandoned) == 0 {
		return nil
	}

	return SecurityAdvisories{Fixed: fixed, Remaining: remaining, Abandoned: abandoned}
}

// --- update_hooks ---

// ReportKey implements report.Reporter.
func (uh *UpdateHooks) ReportKey() string { return "update_hooks" }

// ReportData implements report.Reporter. Keyed by site, like the merge request description:
// in a multisite run the same module can have different pending hooks per site.
func (uh *UpdateHooks) ReportData() any {
	uh.mu.Lock()
	defer uh.mu.Unlock()

	if len(uh.hooks) == 0 {
		return nil
	}

	// Copy: the caller has no way to know this map is mutex-guarded state. Deep, because the
	// per-site maps are guarded too.
	out := make(map[string]map[string]drush.UpdateHook, len(uh.hooks))
	for site, hooks := range uh.hooks {
		out[site] = maps.Clone(hooks)
	}

	return out
}

// --- unsupported_modules ---

// ReportKey implements report.Reporter.
func (um *UnsupportedModules) ReportKey() string { return "unsupported_modules" }

// ReportData implements report.Reporter. Sorted by name so two runs over an unchanged site
// produce byte-identical sections a consumer can diff directly.
func (um *UnsupportedModules) ReportData() any {
	um.mu.Lock()
	defer um.mu.Unlock()

	if len(um.modules) == 0 {
		return nil
	}

	modules := slices.Collect(maps.Values(um.modules))
	slices.SortFunc(modules, func(a, b drush.UnsupportedModule) int { return strings.Compare(a.Name, b.Name) })

	return modules
}

// --- composer_patches ---

// Patches is the composer_patches section. Conflicts are the ones that need a human: a patch
// that no longer applies to the updated package and could not be replaced automatically.
type Patches struct {
	Removed   []RemovedPatch  `json:"removed"`
	Updated   []UpdatedPatch  `json:"updated"`
	Conflicts []ConflictPatch `json:"conflicts"`
}

// ReportKey implements report.Reporter.
func (h *ComposerPatches1) ReportKey() string { return "composer_patches" }

// ReportData implements report.Reporter.
func (h *ComposerPatches1) ReportData() any {
	if !h.patchUpdates.Changes() {
		return nil
	}

	return Patches{
		Removed:   h.patchUpdates.Removed,
		Updated:   h.patchUpdates.Updated,
		Conflicts: h.patchUpdates.Conflicts,
	}
}

// --- code_beautifier ---

// CodingStyleFixes is the code_beautifier section: which files PHPCBF fixed and committed, and
// how many violations PHPCS called fixable beforehand. The two differ when PHPCBF cannot fix a
// violation it reported as fixable.
type CodingStyleFixes struct {
	Files   []string `json:"files"`
	Fixable int      `json:"fixable"`
}

// ReportKey implements report.Reporter.
func (cb *CodeBeautifier) ReportKey() string { return "code_beautifier" }

// ReportData implements report.Reporter.
func (cb *CodeBeautifier) ReportData() any {
	if len(cb.fixedFiles) == 0 {
		return nil
	}
	return CodingStyleFixes{Files: cb.fixedFiles, Fixable: cb.fixable}
}

// --- deprecations_remover ---

// DeprecationFix is one file Rector rewrote, and the rules that fired on it. The rule names are
// the actionable part: they say which deprecation was removed, which "the file changed" does not.
type DeprecationFix struct {
	File           string   `json:"file"`
	AppliedRectors []string `json:"applied_rectors,omitempty"`
}

// ReportKey implements report.Reporter.
func (dr *DeprecationsRemover) ReportKey() string { return "deprecations_remover" }

// ReportData implements report.Reporter.
func (dr *DeprecationsRemover) ReportData() any {
	if len(dr.fixes) == 0 {
		return nil
	}
	return dr.fixes
}

// --- translations_updater ---

// TranslationResult is one site's outcome. Skipped is set when the addon bailed out by design --
// no locale_deploy, or an unresolvable translation path -- which a report that simply omitted
// the site would render indistinguishable from "ran and found nothing".
type TranslationResult struct {
	Path    string `json:"path,omitempty"`
	Updated bool   `json:"updated"`
	Skipped string `json:"skipped,omitempty"`
}

// ReportKey implements report.Reporter.
func (tu *TranslationsUpdater) ReportKey() string { return "translations_updater" }

// ReportData implements report.Reporter. Keyed by site: in a multisite run each site has its own
// translation directory and can succeed or skip independently.
func (tu *TranslationsUpdater) ReportData() any {
	tu.mu.Lock()
	defer tu.mu.Unlock()

	if len(tu.results) == 0 {
		return nil
	}

	// Copy: the caller has no way to know this map is mutex-guarded state.
	return maps.Clone(tu.results)
}
