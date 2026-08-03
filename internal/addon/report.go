package addon

import (
	"maps"
	"slices"
	"strings"

	"github.com/drupdater/drupdater/pkg/composer"
	"github.com/drupdater/drupdater/pkg/drush"
)

// Every addon's contribution to the --report document, together in one file because these
// methods are a published contract and a rename spread over eight files is easy to miss.
//
// Addons satisfy report.Reporter structurally, so none of them imports the report package. Keys
// match .drupdater.yaml's addon names.
//
// composer_diff and composer_normalizer contribute nothing: the top-level "packages" field
// already carries what they do.

// --- composer_audit ---

// SecurityAdvisories is the composer_audit section. Abandoned is not an advisory but the same
// kind of finding, on the non-Drupal packages unsupported_modules cannot see.
type SecurityAdvisories struct {
	Fixed     []composer.Advisory         `json:"fixed"`
	Remaining []composer.Advisory         `json:"remaining"`
	Abandoned []composer.AbandonedPackage `json:"abandoned"`
}

// ReportKey implements report.Reporter.
func (ca *ComposerAudit) ReportKey() string { return "composer_audit" }

// ReportData implements report.Reporter. Both lists arrive sorted, so this stays stable.
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

	// Deep copy: the caller has no way to know these maps are mutex-guarded state.
	out := make(map[string]map[string]drush.UpdateHook, len(uh.hooks))
	for site, hooks := range uh.hooks {
		out[site] = maps.Clone(hooks)
	}

	return out
}

// --- unsupported_modules ---

// ReportKey implements report.Reporter.
func (um *UnsupportedModules) ReportKey() string { return "unsupported_modules" }

// ReportData implements report.Reporter. Sorted so an unchanged site reports byte-identically.
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

// Patches is the composer_patches section. Conflicts are the ones that need a human.
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

// CodingStyleFixes is the code_beautifier section. Files and Fixable differ when PHPCBF cannot
// fix a violation PHPCS reported as fixable.
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

// DeprecationFix is one file Rector rewrote, and the rules that fired — which name the
// deprecation removed, where "the file changed" does not.
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

// TranslationResult is one site's outcome. Skipped records a deliberate bail-out, which omitting
// the site would make indistinguishable from "ran and found nothing".
type TranslationResult struct {
	Path    string `json:"path,omitempty"`
	Updated bool   `json:"updated"`
	Skipped string `json:"skipped,omitempty"`
}

// ReportKey implements report.Reporter.
func (tu *TranslationsUpdater) ReportKey() string { return "translations_updater" }

// ReportData implements report.Reporter. Keyed by site: each has its own translation directory.
func (tu *TranslationsUpdater) ReportData() any {
	tu.mu.Lock()
	defer tu.mu.Unlock()

	if len(tu.results) == 0 {
		return nil
	}

	// Copy: the caller has no way to know this map is mutex-guarded state.
	return maps.Clone(tu.results)
}
