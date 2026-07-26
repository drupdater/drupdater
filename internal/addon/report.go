package addon

import (
	"sort"

	"github.com/drupdater/drupdater/pkg/composer"
	"github.com/drupdater/drupdater/pkg/drush"
)

// This file holds every addon's contribution to the machine-readable run report (--report).
//
// The methods live together rather than beside each addon's other code on purpose: together they
// are the report's addons schema, which is a published contract, and keeping them in one place
// makes it reviewable as a whole. Scattered across eight files it is very easy to change a field
// name without noticing that a consumer depends on it.
//
// Each addon satisfies report.Reporter structurally -- ReportKey() string and ReportData() any --
// so none of them has to import the report package. Addons that do not implement it are simply
// absent from the report.
//
// The keys match the names used in .drupdater.yaml's addon lists, so a report reads the same way
// the project is configured.
//
// Two addons deliberately contribute nothing:
//
//   - composer_diff renders a markdown dependency table for the merge request. The same
//     information is already in the report's top-level "packages" field, in structured form,
//     straight from composer update -- a markdown table in a JSON document would be strictly
//     worse than what is already there.
//   - composer_normalizer only reorders composer.json; that a run normalised it is visible in
//     the diff and has no consumer beyond it.
//
// Everything else reports, including addons whose work is "only" a code change. Reading the
// diff tells you what changed but not whether the addon ran at all, and most addons log and
// swallow their own failures -- so an addon that silently did nothing is indistinguishable
// from one with nothing to do unless it says so here.

// --- composer_audit ---

// SecurityAdvisories is the composer_audit section: which advisories the update resolved and
// which are still open afterwards. The remaining advisories are the more actionable half — they
// are what a fleet-wide exposure view is built from.
type SecurityAdvisories struct {
	Fixed     []composer.Advisory `json:"fixed"`
	Remaining []composer.Advisory `json:"remaining"`
}

// ReportKey implements report.Reporter.
func (ca *ComposerAudit) ReportKey() string { return "composer_audit" }

// ReportData implements report.Reporter.
func (ca *ComposerAudit) ReportData() any {
	fixed := ca.GetFixedAdvisories()
	remaining := ca.afterAudit.Advisories
	if len(fixed) == 0 && len(remaining) == 0 {
		return nil
	}

	return SecurityAdvisories{Fixed: fixed, Remaining: remaining}
}

// --- update_hooks ---

// ReportKey implements report.Reporter.
func (uh *UpdateHooks) ReportKey() string { return "update_hooks" }

// ReportData implements report.Reporter. The result is keyed by site, matching the shape the
// merge request description uses, because in a multisite run the same module can have different
// pending hooks per site.
func (uh *UpdateHooks) ReportData() any {
	uh.mu.Lock()
	defer uh.mu.Unlock()

	if len(uh.hooks) == 0 {
		return nil
	}

	// Copy rather than hand out the live map: the report is serialised after the run, but the
	// caller has no way to know that this map is mutex-guarded state.
	out := make(map[string]map[string]drush.UpdateHook, len(uh.hooks))
	for site, hooks := range uh.hooks {
		siteHooks := make(map[string]drush.UpdateHook, len(hooks))
		for name, hook := range hooks {
			siteHooks[name] = hook
		}
		out[site] = siteHooks
	}

	return out
}

// --- unsupported_modules ---

// ReportKey implements report.Reporter.
func (um *UnsupportedModules) ReportKey() string { return "unsupported_modules" }

// ReportData implements report.Reporter. Modules are sorted by name so two runs over an
// unchanged site produce byte-identical sections, which lets a consumer diff reports directly
// instead of having to normalise map ordering first.
func (um *UnsupportedModules) ReportData() any {
	um.mu.Lock()
	defer um.mu.Unlock()

	if len(um.modules) == 0 {
		return nil
	}

	modules := make([]drush.UnsupportedModule, 0, len(um.modules))
	for _, module := range um.modules {
		modules = append(modules, module)
	}
	sort.Slice(modules, func(i, j int) bool { return modules[i].Name < modules[j].Name })

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

// CodingStyleFixes is the code_beautifier section: which files PHPCBF actually fixed and
// committed, and how many violations PHPCS considered fixable beforehand. The two differ when a
// violation is reported fixable but PHPCBF cannot fix it in practice.
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

// TranslationResult is one site's outcome. Skipped is set when the addon bailed out early, which
// it does by design when locale_deploy is not enabled or the translation path does not resolve --
// both previously visible only in the logs, and both indistinguishable from "ran and found
// nothing" in a report that simply omitted the site.
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

	// Copy rather than hand out the live map, which is mutex-guarded state the caller cannot
	// know about.
	out := make(map[string]TranslationResult, len(tu.results))
	for site, result := range tu.results {
		out[site] = result
	}
	return out
}
