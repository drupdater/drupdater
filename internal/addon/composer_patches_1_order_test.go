package addon

import (
	"context"
	"testing"

	"github.com/drupdater/drupdater/internal/composer"
	"github.com/drupdater/drupdater/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// composer-patches 1.x applies a package's patches in declaration order, so a set that only
// applies in that order must be checked in it.
func TestCombinedPatchesAreCheckedInDeclarationOrder(t *testing.T) {
	composerService := NewMockPatchesComposer(t)
	composerService.EXPECT().CheckIfPatchesApply(anyCtx, "/tmp", "drupal/core", "8.8.0", []string{
		"/tmp/patches/z.patch", "https://example.com/m.patch", "/tmp/patches/a.patch",
	}).Return(true, nil)
	updater := &ComposerPatches1{logger: zap.NewNop(), composer: composerService}

	patches := declaredPatches{packages: []string{"drupal/core"}, sets: map[string][]patchEntry{"drupal/core": {
		{"z", "patches/z.patch"}, {"m", "https://example.com/m.patch"}, {"a", "patches/a.patch"},
	}}}
	updates := PatchUpdates{}
	updater.validateCombinedPatches(t.Context(), "/tmp", upgradeCore()[0], patches, &updates)

	assert.Empty(t, updates.Conflicts)
}

// Rewriting extra.patches used to sort it by description, silently changing the order the
// project's patches are applied in.
func TestWritingPatchesBackKeepsTheirOrderAndTheRenamedOnesPlace(t *testing.T) {
	const declared = `{"drupal/token":{"z later fix":"patches/z.patch","Issue #123456":"patches/fixed.patch",` +
		`"a earlier fix":"patches/a.patch","see 222222":"patches/b.patch"},"drupal/core":{"only":"patches/core.patch"}}`

	composerService := NewMockPatchesComposer(t)
	composerService.EXPECT().Project(anyCtx, "/repo").Return(patchedProject(declared, nil, "drupal/token", "drupal/core"), nil)
	composerService.EXPECT().Update(anyCtx, "/repo", []string(nil), []string(nil), false, true).
		Return(upgradeToken(), nil)
	composerService.EXPECT().CheckIfPatchApplies(anyCtx, "/repo", "drupal/token", "1.15.0", mock.Anything).Return(true, nil)
	composerService.EXPECT().CheckIfPatchesApply(anyCtx, "/repo", "drupal/token", "1.15.0",
		[]string{"/repo/patches/z.patch", "/repo/patches/a.patch", "/repo/patches/b.patch"}).Return(true, nil)
	var written string
	composerService.EXPECT().SetConfig(anyCtx, "/repo", "extra.patches", mock.Anything).
		Run(func(_ context.Context, _, _, value string) { written = value }).Return(nil)
	composerService.EXPECT().UpdateLockHash(anyCtx, "/repo").Return(nil)

	drupalOrgService := NewMockDrupalOrg(t)
	drupalOrgService.EXPECT().FindIssueNumber("Issue #123456").Return("123456", true)
	drupalOrgService.EXPECT().GetIssue(anyCtx, "123456").Return(fixedIssue(), nil)
	drupalOrgService.EXPECT().FindIssueNumber("see 222222").Return("222222", true)
	other := fixedIssue()
	other.ID, other.Title, other.Status, other.URL = "222222", "Other", "1", "https://www.drupal.org/node/222222"
	drupalOrgService.EXPECT().GetIssue(anyCtx, "222222").Return(other, nil)
	drupalOrgService.EXPECT().FindIssueNumber(mock.Anything).Return("", false)
	drupalCode := NewMockDrupalCode(t)
	drupalCode.EXPECT().IsFixedIn(anyCtx, "drupal", "123456", "1.15.0").Return(true, nil)

	var removed []string
	h := &ComposerPatches1{logger: zap.NewNop(), composer: composerService, drupalOrg: drupalOrgService,
		drupalCode: drupalCode, removeFile: recordRemovals(&removed)}
	_, err := h.preComposerUpdateHandler(workflow.NewPreComposerUpdateEvent(t.Context(), "/repo", &fakeCommitter{}, workflow.UpdatePlan{}))

	require.NoError(t, err)
	//nolint:testifylint // JSONEq ignores key order, which is what this asserts.
	assert.Equal(t, `{"drupal/token":{"z later fix":"patches/z.patch","a earlier fix":"patches/a.patch",`+
		`"Issue #222222: [Other](https://www.drupal.org/node/222222)":"patches/b.patch"},"drupal/core":{"only":"patches/core.patch"}}`,
		written)
}

func TestARenameThatWouldCollideKeepsTheOriginalDescription(t *testing.T) {
	const canonical = "Issue #123456: [Alot of problems](https://www.drupal.org/node/123456)"
	composerService := NewMockPatchesComposer(t)
	composerService.EXPECT().CheckIfPatchApplies(anyCtx, "/tmp", "drupal/core", "8.8.0", mock.Anything).Return(true, nil)
	composerService.EXPECT().CheckIfPatchesApply(anyCtx, "/tmp", "drupal/core", "8.8.0", mock.Anything).Return(true, nil)
	drupalOrgService := NewMockDrupalOrg(t)
	drupalOrgService.EXPECT().FindIssueNumber(mock.Anything).Return("123456", true)
	open := fixedIssue()
	open.Status = "1"
	drupalOrgService.EXPECT().GetIssue(anyCtx, "123456").Return(open, nil)

	updater := &ComposerPatches1{logger: zap.NewNop(), composer: composerService, drupalOrg: drupalOrgService}
	patches := declaredPatches{packages: []string{"drupal/core"}, sets: map[string][]patchEntry{"drupal/core": {
		{"first part of #123456", "patches/1.patch"}, {"second part of #123456", "patches/2.patch"},
	}}}

	_, result := updater.updatePatches(t.Context(), "/tmp", lockedProject("drupal/core"), upgradeCore(), patches)

	assert.Equal(t, []patchEntry{{canonical, "patches/1.patch"}, {"second part of #123456", "patches/2.patch"}},
		result.sets["drupal/core"], "renaming both would leave one key for two patches, and lose one")
}

func upgradeToken() []composer.PackageChange {
	return []composer.PackageChange{{Action: "Upgrade", Package: "drupal/token", From: "1.14.0", To: "1.15.0"}}
}

func TestRemovalsAreReportedInFullAndInDeclarationOrder(t *testing.T) {
	var removed []string
	h := &ComposerPatches1{logger: zap.NewNop(), removeFile: recordRemovals(&removed)}
	declared := func() *declaredPatches {
		return &declaredPatches{packages: []string{"drupal/gone", "drupal/kept"}, sets: map[string][]patchEntry{
			"drupal/gone": {{"z fix", "patches/z.patch"}, {"a fix", "https://example.com/a.patch"}},
			"drupal/kept": {{"dup", "https://example.com/dup.patch"}, {"own", "https://example.com/own.patch"}},
		}}
	}

	patches := declared()
	assert.Equal(t, []RemovedPatch{
		{Package: "drupal/gone", PatchDescription: "z fix", PatchPath: "patches/z.patch", Reason: "drupal/gone is not installed in the project"},
		{Package: "drupal/gone", PatchDescription: "a fix", PatchPath: "https://example.com/a.patch", Reason: "drupal/gone is not installed in the project"},
	}, h.removeUninstalledPackagePatches("/p", lockedProject("drupal/kept"), patches))
	assert.Equal(t, []string{"drupal/kept"}, patches.packages)
	assert.Equal(t, []string{"/p/patches/z.patch"}, removed, "a URL has no file to remove")

	patches = declared()
	project := lockedProject("drupal/gone", "drupal/kept")
	project.DependencyPatches = map[string][]string{"drupal/kept": {"https://example.com/dup.patch"}}
	assert.Equal(t, []RemovedPatch{
		{Package: "drupal/kept", PatchDescription: "dup", PatchPath: "https://example.com/dup.patch", Reason: "Patch is already applied by a dependency of drupal/kept"},
	}, h.removeDependencyProvidedPatches(project, patches))
	assert.Equal(t, []patchEntry{{"own", "https://example.com/own.patch"}}, patches.sets["drupal/kept"])

	patches = declared()
	assert.Equal(t, []RemovedPatch{
		{Package: "drupal/gone", PatchDescription: "z fix", PatchPath: "patches/z.patch", Reason: "drupal/gone is no longer installed"},
		{Package: "drupal/gone", PatchDescription: "a fix", PatchPath: "https://example.com/a.patch", Reason: "drupal/gone is no longer installed"},
	}, h.removePackagePatches("/p", composer.PackageChange{Action: "Remove", Package: "drupal/gone"}, patches))
	assert.Equal(t, []string{"drupal/kept"}, patches.packages)
}
