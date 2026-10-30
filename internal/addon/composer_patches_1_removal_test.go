package addon

import (
	"context"
	"testing"

	"github.com/drupdater/drupdater/internal/composer"
	"github.com/drupdater/drupdater/internal/drupalorg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// fixedInTarget is a drupalcode whose history has issue 123456 fixed in drupal/core 8.8.0.
func fixedInTarget(t *testing.T) *MockDrupalCode {
	t.Helper()
	drupalCode := NewMockDrupalCode(t)
	drupalCode.EXPECT().IsFixedIn(anyCtx, "drupal", "123456", "8.8.0").Return(true, nil)
	return drupalCode
}

func fixedIssue() *drupalorg.Issue {
	issue := &drupalorg.Issue{
		ID:     "123456",
		Title:  "Alot of problems",
		Status: "7", // Closed (fixed)
		URL:    "https://www.drupal.org/node/123456",
	}
	issue.Project.MaschineName = "drupal"
	return issue
}

func upgradeCore() []composer.PackageChange {
	return []composer.PackageChange{{
		Action:  "Upgrade",
		Package: "drupal/core",
		From:    "8.7.0",
		To:      "8.8.0",
	}}
}

func TestUpdatePatchesRemovesRemotePatchForFixedIssue(t *testing.T) {
	// A remote patch has no file to remove. Passing its URL to the file removal used to fail,
	// dropping the patch from composer.json unreported.
	const patchURL = "https://www.drupal.org/files/issues/123456-1.patch"

	composerService := NewMockPatchesComposer(t)
	composerService.EXPECT().GetDependencyPatches(anyCtx, "/tmp").Return(nil, nil).Maybe()
	composerService.EXPECT().IsPackageInstalled(anyCtx, "/tmp", "drupal/core").Return(true, nil)

	drupalOrgService := NewMockDrupalOrg(t)
	drupalOrgService.EXPECT().FindIssueNumber("Issue #123456").Return("123456", true)
	drupalOrgService.EXPECT().GetIssue(anyCtx, "123456").Return(fixedIssue(), nil)

	var removed []string
	updater := &ComposerPatches1{
		logger:     zap.NewNop(),
		composer:   composerService,
		drupalOrg:  drupalOrgService,
		drupalCode: fixedInTarget(t),
		removeFile: recordRemovals(&removed),
	}

	patches := map[string]map[string]string{
		"drupal/core": {"Issue #123456": patchURL},
	}

	report, newPatches := updater.updatePatches(t.Context(), "/tmp", upgradeCore(), patches)

	assert.Empty(t, removed, "a URL is not a file; removing it is exactly the bug under test")
	assert.Equal(t, map[string]map[string]string{}, newPatches, "the patch must be dropped from composer.json")
	require.Len(t, report.Removed, 1, "and the removal must be reported in the merge request")
	assert.Equal(t, patchURL, report.Removed[0].PatchPath)
	assert.Equal(t, "drupal/core", report.Removed[0].Package)
	assert.Contains(t, report.Removed[0].Reason, "is fixed in drupal/core 8.8.0")
}

func TestUpdatePatchesKeepsPatchWhenRemovalFails(t *testing.T) {
	// If the patch file cannot be removed the entry must stay declared. Dropping it silently
	// would change composer.json without any trace in the merge request description.
	const patchPath = "patches/drupal/123456.patch"

	composerService := NewMockPatchesComposer(t)
	composerService.EXPECT().GetDependencyPatches(anyCtx, "/tmp").Return(nil, nil).Maybe()
	composerService.EXPECT().IsPackageInstalled(anyCtx, "/tmp", "drupal/core").Return(true, nil)

	drupalOrgService := NewMockDrupalOrg(t)
	drupalOrgService.EXPECT().FindIssueNumber("Issue #123456").Return("123456", true)
	drupalOrgService.EXPECT().GetIssue(anyCtx, "123456").Return(fixedIssue(), nil)

	updater := &ComposerPatches1{
		logger:     zap.NewNop(),
		composer:   composerService,
		drupalOrg:  drupalOrgService,
		drupalCode: fixedInTarget(t),
		removeFile: func(string) error { return assert.AnError },
	}

	patches := map[string]map[string]string{
		"drupal/core": {"Issue #123456": patchPath},
	}

	report, newPatches := updater.updatePatches(t.Context(), "/tmp", upgradeCore(), patches)

	assert.Empty(t, report.Removed)
	assert.Equal(t, map[string]map[string]string{
		"drupal/core": {"Issue #123456": patchPath},
	}, newPatches, "the patch must still be declared")
}

func TestDropPatchFile(t *testing.T) {
	t.Run("skips remote patches", func(t *testing.T) {
		var removed []string
		updater := &ComposerPatches1{logger: zap.NewNop(), removeFile: recordRemovals(&removed)}
		require.NoError(t, updater.dropPatchFile("/project", "https://example.com/a.patch"))
		assert.Empty(t, removed)
	})

	t.Run("removes local patches below the project", func(t *testing.T) {
		var removed []string
		updater := &ComposerPatches1{logger: zap.NewNop(), removeFile: recordRemovals(&removed)}
		require.NoError(t, updater.dropPatchFile("/project", "patches/a.patch"))
		assert.Equal(t, []string{"/project/patches/a.patch"}, removed)
	})

	t.Run("propagates the removal error", func(t *testing.T) {
		updater := &ComposerPatches1{logger: zap.NewNop(), removeFile: func(string) error { return assert.AnError }}
		require.ErrorIs(t, updater.dropPatchFile("/project", "patches/a.patch"), assert.AnError)
	})
}

func TestValidateCombinedPatchesResolvesAbsoluteLocalPaths(t *testing.T) {
	// url.ParseRequestURI accepts an absolute local path, which once passed through unprefixed
	// and pinned the package on a conflict that did not exist.
	composerService := NewMockPatchesComposer(t)
	composerService.EXPECT().
		CheckIfPatchesApply(mock.Anything, mock.Anything, "drupal/core", "8.8.0", mock.Anything).
		RunAndReturn(func(_ context.Context, _ string, _ string, _ string, paths []string) (bool, error) {
			assert.ElementsMatch(t, []string{
				"/tmp/patches/local.patch",
				"/tmp//absolute/local.patch",
				"https://example.com/remote.patch",
			}, paths)
			return true, nil
		})

	updater := &ComposerPatches1{logger: zap.NewNop(), composer: composerService}

	patches := map[string]map[string]string{
		"drupal/core": {
			"relative": "patches/local.patch",
			"absolute": "/absolute/local.patch",
			"remote":   "https://example.com/remote.patch",
		},
	}
	updates := PatchUpdates{}

	updater.validateCombinedPatches(t.Context(), "/tmp", upgradeCore()[0], patches, &updates)

	assert.Empty(t, updates.Conflicts)
}

func TestUpdatePatchesRecognisesEveryFixedIssueStatus(t *testing.T) {
	// Three drupal.org statuses mean "the fix has landed" -- 2, 7 and 15 -- each checked
	// separately in the guard, so each needs its own case. The negative matters as much: an
	// open issue must keep its patch, or a live fix is dropped from composer.json.
	tests := []struct {
		name        string
		status      string
		wantRemoved bool
	}{
		{name: "Fixed", status: "2", wantRemoved: true},
		{name: "Closed (fixed)", status: "7", wantRemoved: true},
		{name: "Patch (to be ported)", status: "15", wantRemoved: true},
		{name: "still open", status: "1", wantRemoved: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const patchURL = "https://www.drupal.org/files/issues/123456-1.patch"

			composerService := NewMockPatchesComposer(t)
			composerService.EXPECT().GetDependencyPatches(anyCtx, "/tmp").Return(nil, nil).Maybe()
			composerService.EXPECT().IsPackageInstalled(anyCtx, "/tmp", "drupal/core").Return(true, nil)
			if !tt.wantRemoved {
				// An open issue keeps its patch, so the patch is re-checked against the new version.
				composerService.EXPECT().CheckIfPatchApplies(anyCtx, "/tmp", "drupal/core", "8.8.0", patchURL).
					Return(true, nil).Maybe()
				composerService.EXPECT().CheckIfPatchesApply(anyCtx, "/tmp", "drupal/core", "8.8.0", mock.Anything).
					Return(true, nil).Maybe()
			}

			issue := fixedIssue()
			issue.Status = tt.status

			drupalOrgService := NewMockDrupalOrg(t)
			drupalOrgService.EXPECT().FindIssueNumber("Issue #123456").Return("123456", true)
			drupalOrgService.EXPECT().GetIssue(anyCtx, "123456").Return(issue, nil)

			// An open issue's fix is never looked up: only a fixed status asks drupalcode.
			drupalCode := NewMockDrupalCode(t)
			if tt.wantRemoved {
				drupalCode.EXPECT().IsFixedIn(anyCtx, "drupal", "123456", "8.8.0").Return(true, nil)
			}
			updater := &ComposerPatches1{
				logger:     zap.NewNop(),
				composer:   composerService,
				drupalOrg:  drupalOrgService,
				drupalCode: drupalCode,
			}

			patches := map[string]map[string]string{"drupal/core": {"Issue #123456": patchURL}}
			report, newPatches := updater.updatePatches(t.Context(), "/tmp", upgradeCore(), patches)

			if tt.wantRemoved {
				assert.Empty(t, newPatches["drupal/core"], "a fixed issue's patch must be dropped")
				require.Len(t, report.Removed, 1)
				assert.Contains(t, report.Removed[0].Reason, "8.8.0")
			} else {
				assert.NotEmpty(t, newPatches["drupal/core"], "an open issue's patch must be kept")
				assert.Empty(t, report.Removed)
			}
		})
	}
}

func TestUpdatePatchesKeepsPatchWithoutDrupalCode(t *testing.T) {
	// Without drupalcode the fix cannot be confirmed, so the patch stays. Dropping it on
	// the issue status alone would remove patches whose fix is not in the target version yet.
	const patchURL = "https://www.drupal.org/files/issues/123456-1.patch"

	composerService := NewMockPatchesComposer(t)
	composerService.EXPECT().GetDependencyPatches(anyCtx, "/tmp").Return(nil, nil).Maybe()
	composerService.EXPECT().IsPackageInstalled(anyCtx, "/tmp", "drupal/core").Return(true, nil)
	composerService.EXPECT().CheckIfPatchApplies(anyCtx, "/tmp", "drupal/core", "8.8.0", patchURL).Return(true, nil).Maybe()
	composerService.EXPECT().CheckIfPatchesApply(anyCtx, "/tmp", "drupal/core", "8.8.0", mock.Anything).Return(true, nil).Maybe()

	drupalOrgService := NewMockDrupalOrg(t)
	drupalOrgService.EXPECT().FindIssueNumber("Issue #123456").Return("123456", true)
	drupalOrgService.EXPECT().GetIssue(anyCtx, "123456").Return(fixedIssue(), nil)

	updater := &ComposerPatches1{
		logger:     zap.NewNop(),
		composer:   composerService,
		drupalOrg:  drupalOrgService,
		drupalCode: nil,
	}

	patches := map[string]map[string]string{"drupal/core": {"Issue #123456": patchURL}}
	_, newPatches := updater.updatePatches(t.Context(), "/tmp", upgradeCore(), patches)

	assert.NotEmpty(t, newPatches["drupal/core"])
}
