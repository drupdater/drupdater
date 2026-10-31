package addon

import (
	"maps"
	"slices"
	"strings"

	"github.com/drupdater/drupdater/internal/composer"
	"github.com/drupdater/drupdater/internal/drush"
	"github.com/drupdater/drupdater/internal/workflow"
)

// Every addon's contribution to the --report document, together in one file because these
// methods are a published contract and a rename spread over eight files is easy to miss.
//
// Each implements workflow.Reporter, reading what its hooks returned from the pipeline's
// findings. Keys match .drupdater.yaml's addon names.
//
// composer_normalizer contributes nothing: what it changes is formatting, not a finding.

// --- composer_audit ---

// SecurityAdvisories is the composer_audit section. Abandoned is not an advisory but the same
// kind of finding, on the non-Drupal packages unsupported_modules cannot see.
type SecurityAdvisories struct {
	Fixed     []composer.Advisory         `json:"fixed"`
	Remaining []composer.Advisory         `json:"remaining"`
	Abandoned []composer.AbandonedPackage `json:"abandoned"`
}

// ReportKey implements workflow.Reporter.
func (ca *ComposerAudit) ReportKey() string { return "composer_audit" }

// ReportData implements workflow.Reporter. Both lists arrive sorted, so this stays stable.
func (ca *ComposerAudit) ReportData(findings *workflow.Findings) any {
	after, audited := workflow.Shared[composer.Audit](findings, workflow.EventPostCodeUpdate)
	// Without the after-audit every advisory found before would read as fixed.
	if !audited {
		return nil
	}
	before, _ := workflow.Shared[composer.Audit](findings, workflow.EventPreComposerUpdate)
	fixed := fixedAdvisories(before.Finding, after.Finding)
	remaining := after.Finding.Advisories
	abandoned := abandonedPackages(after.Finding)
	if len(fixed) == 0 && len(remaining) == 0 && len(abandoned) == 0 {
		return nil
	}

	return SecurityAdvisories{Fixed: fixed, Remaining: remaining, Abandoned: abandoned}
}

// --- composer_diff ---

// DependencyChange is one row of the composer_diff section. Unlike the top-level packages, which
// come from composer's own output, it separates dev packages and links to each change.
type DependencyChange struct {
	Package string `json:"package"`
	// Operation is install, upgrade, downgrade, remove, or change: a new commit of one version.
	Operation  string `json:"operation"`
	From       string `json:"from,omitempty"`
	To         string `json:"to,omitempty"`
	Dev        bool   `json:"dev"`
	Direct     bool   `json:"direct"`
	CompareURL string `json:"compare_url,omitempty"`
	ProjectURL string `json:"project_url,omitempty"`
}

// ReportKey implements workflow.Reporter.
func (cd *ComposerDiff) ReportKey() string { return "composer_diff" }

// ReportData implements workflow.Reporter.
func (cd *ComposerDiff) ReportData(findings *workflow.Findings) any {
	recorded, _ := workflow.Shared[[]composer.DiffEntry](findings, workflow.EventPostComposerUpdate)
	entries := recorded.Finding
	if len(entries) == 0 {
		return nil
	}

	changes := make([]DependencyChange, 0, len(entries))
	for _, e := range entries {
		changes = append(changes, DependencyChange{
			Package: e.Package, Operation: e.Operation, From: e.From, To: e.To,
			Dev: e.Dev, Direct: e.Direct, CompareURL: e.CompareURL, ProjectURL: e.ProjectURL,
		})
	}
	return changes
}

// --- composer_allow_plugins ---

// PluginAllowlist is the composer_allow_plugins section. Added are the plugins the update brought
// in, entered into allow-plugins as false: each needs someone to decide whether to trust it.
type PluginAllowlist struct {
	Added []string `json:"added"`
}

// ReportKey implements workflow.Reporter.
func (ap *ComposerAllowPlugins) ReportKey() string { return "composer_allow_plugins" }

// ReportData implements workflow.Reporter. Sorted: the plugins are found by ranging over a map.
func (ap *ComposerAllowPlugins) ReportData(findings *workflow.Findings) any {
	recorded, _ := workflow.Shared[[]string](findings, workflow.EventPostComposerUpdate)
	if len(recorded.Finding) == 0 {
		return nil
	}
	return PluginAllowlist{Added: slices.Sorted(slices.Values(recorded.Finding))}
}

// --- update_hooks ---

// ReportKey implements workflow.Reporter.
func (uh *UpdateHooks) ReportKey() string { return "update_hooks" }

// ReportData implements workflow.Reporter. Keyed by site, like the merge request description:
// in a multisite run the same module can have different pending hooks per site.
func (uh *UpdateHooks) ReportData(findings *workflow.Findings) any {
	hooks := workflow.SiteFindings[map[string]drush.UpdateHook](findings, workflow.EventPreSiteUpdate)
	maps.DeleteFunc(hooks, func(_ string, siteHooks map[string]drush.UpdateHook) bool { return len(siteHooks) == 0 })
	if len(hooks) == 0 {
		return nil
	}
	return hooks
}

// --- unsupported_modules ---

// ReportKey implements workflow.Reporter.
func (um *UnsupportedModules) ReportKey() string { return "unsupported_modules" }

// ReportData implements workflow.Reporter. One entry per module, however many sites have it, and
// sorted so an unchanged project reports byte-identically.
func (um *UnsupportedModules) ReportData(findings *workflow.Findings) any {
	byName := map[string]drush.UnsupportedModule{}
	for _, modules := range workflow.SiteFindings[[]drush.UnsupportedModule](findings, workflow.EventPreSiteUpdate) {
		for _, module := range modules {
			byName[module.Name] = module
		}
	}
	if len(byName) == 0 {
		return nil
	}

	modules := slices.Collect(maps.Values(byName))
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

// ReportKey implements workflow.Reporter.
func (h *ComposerPatches1) ReportKey() string { return "composer_patches" }

// ReportData implements workflow.Reporter.
func (h *ComposerPatches1) ReportData(findings *workflow.Findings) any {
	recorded, _ := workflow.Shared[PatchUpdates](findings, workflow.EventPreComposerUpdate)
	updates := recorded.Finding
	if !updates.Changes() {
		return nil
	}

	return Patches(updates)
}

// --- code_beautifier ---

// CodingStyleFixes is the code_beautifier section. Files and Fixable differ when PHPCBF cannot
// fix a violation PHPCS reported as fixable.
type CodingStyleFixes struct {
	Files   []string `json:"files"`
	Fixable int      `json:"fixable"`
}

// ReportKey implements workflow.Reporter.
func (cb *CodeBeautifier) ReportKey() string { return "code_beautifier" }

// ReportData implements workflow.Reporter. Fixes whose commit was not made never reached the branch.
func (cb *CodeBeautifier) ReportData(findings *workflow.Findings) any {
	recorded, _ := workflow.Shared[CodingStyleFixes](findings, workflow.EventPostCodeUpdate)
	if !slices.Contains(recorded.Committed, codingStylesCommitMessage) {
		return nil
	}
	return recorded.Finding
}

// --- deprecations_remover ---

// DeprecationFix is one file Rector rewrote, and the rules that fired — which name the
// deprecation removed, where "the file changed" does not.
type DeprecationFix struct {
	File           string   `json:"file"`
	AppliedRectors []string `json:"applied_rectors,omitempty"`
}

// ReportKey implements workflow.Reporter.
func (dr *DeprecationsRemover) ReportKey() string { return "deprecations_remover" }

// ReportData implements workflow.Reporter.
func (dr *DeprecationsRemover) ReportData(findings *workflow.Findings) any {
	recorded, _ := workflow.Shared[[]DeprecationFix](findings, workflow.EventPostCodeUpdate)
	if len(recorded.Finding) == 0 {
		return nil
	}
	return recorded.Finding
}

// --- translations_updater ---

// TranslationResult is one site's outcome. Skipped records a deliberate bail-out, which omitting
// the site would make indistinguishable from "ran and found nothing".
type TranslationResult struct {
	Path    string `json:"path,omitempty"`
	Updated bool   `json:"updated"`
	Skipped string `json:"skipped,omitempty"`
}

// ReportKey implements workflow.Reporter.
func (tu *TranslationsUpdater) ReportKey() string { return "translations_updater" }

// ReportData implements workflow.Reporter. Keyed by site: each has its own translation directory.
func (tu *TranslationsUpdater) ReportData(findings *workflow.Findings) any {
	recorded := workflow.Sites[TranslationResult](findings, workflow.EventPostSiteUpdate)
	if len(recorded) == 0 {
		return nil
	}
	results := make(map[string]TranslationResult, len(recorded))
	for site, r := range recorded {
		result := r.Finding
		result.Updated = slices.Contains(r.Committed, translationsCommitMessage)
		results[site] = result
	}
	return results
}
