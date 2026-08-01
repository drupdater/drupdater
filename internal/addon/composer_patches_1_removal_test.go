package addon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/drupdater/drupdater/pkg/composer"
	"github.com/drupdater/drupdater/pkg/drupalorg"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	gitlab "gitlab.com/gitlab-org/api/client-go"
	"go.uber.org/zap"
)

// fixedIssueGitlab returns a gitlab client whose commit search reports the issue as fixed in
// the target version.
func fixedIssueGitlab(t *testing.T) *gitlab.Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		var body []byte
		if r.URL.Path == "/api/v4/projects/project/drupal/-/search" {
			body, _ = json.Marshal([]gitlab.Commit{{ID: "5678"}})
		}
		_, err := w.Write(body)
		assert.NoError(t, err)
	}))
	t.Cleanup(server.Close)

	client, err := gitlab.NewClient("", gitlab.WithBaseURL(server.URL))
	require.NoError(t, err)
	return client
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
	// A remote patch has no file in the repository, so there is nothing to remove from the
	// worktree — passing its URL to worktree.Remove would fail, and the patch used to be
	// dropped from composer.json without ever being reported in the merge request.
	const patchURL = "https://www.drupal.org/files/issues/123456-1.patch"

	composerService := NewMockComposer(t)
	composerService.EXPECT().GetDependencyPatches(anyCtx, "/tmp").Return(nil, nil).Maybe()
	composerService.EXPECT().IsPackageInstalled(anyCtx, "/tmp", "drupal/core").Return(true, nil)

	drupalOrgService := NewMockDrupalOrg(t)
	drupalOrgService.EXPECT().FindIssueNumber("Issue #123456").Return("123456", true)
	drupalOrgService.EXPECT().GetIssue(anyCtx, "123456").Return(fixedIssue(), nil)

	// No worktree.Remove expectation: calling it with a URL is exactly the bug under test, and
	// the mock fails the test if it is called.
	worktree := NewMockWorktree(t)

	updater := &ComposerPatches1{
		logger:    zap.NewNop(),
		composer:  composerService,
		drupalOrg: drupalOrgService,
		gitlab:    fixedIssueGitlab(t),
	}

	patches := map[string]map[string]string{
		"drupal/core": {"Issue #123456": patchURL},
	}

	report, newPatches := updater.updatePatches(t.Context(), "/tmp", worktree, upgradeCore(), patches)

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

	composerService := NewMockComposer(t)
	composerService.EXPECT().GetDependencyPatches(anyCtx, "/tmp").Return(nil, nil).Maybe()
	composerService.EXPECT().IsPackageInstalled(anyCtx, "/tmp", "drupal/core").Return(true, nil)

	drupalOrgService := NewMockDrupalOrg(t)
	drupalOrgService.EXPECT().FindIssueNumber("Issue #123456").Return("123456", true)
	drupalOrgService.EXPECT().GetIssue(anyCtx, "123456").Return(fixedIssue(), nil)

	worktree := NewMockWorktree(t)
	worktree.EXPECT().Remove(patchPath).Return(plumbing.NewHash(""), assert.AnError)

	updater := &ComposerPatches1{
		logger:    zap.NewNop(),
		composer:  composerService,
		drupalOrg: drupalOrgService,
		gitlab:    fixedIssueGitlab(t),
	}

	patches := map[string]map[string]string{
		"drupal/core": {"Issue #123456": patchPath},
	}

	report, newPatches := updater.updatePatches(t.Context(), "/tmp", worktree, upgradeCore(), patches)

	assert.Empty(t, report.Removed)
	assert.Equal(t, map[string]map[string]string{
		"drupal/core": {"Issue #123456": patchPath},
	}, newPatches, "the patch must still be declared")
}

func TestDropPatchFile(t *testing.T) {
	updater := &ComposerPatches1{logger: zap.NewNop()}

	t.Run("skips remote patches", func(t *testing.T) {
		// A mock with no Remove expectation fails the test if Remove is called.
		worktree := NewMockWorktree(t)
		require.NoError(t, updater.dropPatchFile(worktree, "https://example.com/a.patch"))
	})

	t.Run("removes local patches", func(t *testing.T) {
		worktree := NewMockWorktree(t)
		worktree.EXPECT().Remove("patches/a.patch").Return(plumbing.NewHash(""), nil)
		require.NoError(t, updater.dropPatchFile(worktree, "patches/a.patch"))
	})

	t.Run("propagates the removal error", func(t *testing.T) {
		worktree := NewMockWorktree(t)
		worktree.EXPECT().Remove("patches/a.patch").Return(plumbing.NewHash(""), assert.AnError)
		require.Error(t, updater.dropPatchFile(worktree, "patches/a.patch"))
	})
}

func TestValidateCombinedPatchesResolvesAbsoluteLocalPaths(t *testing.T) {
	// An absolute local path is not a remote patch. url.ParseRequestURI accepts it, so the
	// previous check passed it through unprefixed, composer could not find it, and the package
	// was pinned on a patch conflict that did not exist.
	composerService := NewMockComposer(t)
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
	// drupal.org has three statuses that mean "the fix has landed": 2 (Fixed), 7 (Closed
	// (fixed)) and 15 (Patch (to be ported)). Each is checked separately in the guard, so each
	// needs its own case -- otherwise two of the three could be deleted and patches for issues
	// closed under those statuses would be carried forward forever.
	//
	// The negative case matters just as much: an issue still open must keep its patch, or a
	// live fix would be dropped from composer.json.
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

			composerService := NewMockComposer(t)
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

			updater := &ComposerPatches1{
				logger:    zap.NewNop(),
				composer:  composerService,
				drupalOrg: drupalOrgService,
				gitlab:    fixedIssueGitlab(t),
			}

			patches := map[string]map[string]string{"drupal/core": {"Issue #123456": patchURL}}
			report, newPatches := updater.updatePatches(t.Context(), "/tmp", NewMockWorktree(t), upgradeCore(), patches)

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

func TestUpdatePatchesKeepsPatchWithoutGitlabClient(t *testing.T) {
	// Without a gitlab client the fix cannot be confirmed, so the patch stays. Dropping it on
	// the issue status alone would remove patches whose fix is not in the target version yet.
	const patchURL = "https://www.drupal.org/files/issues/123456-1.patch"

	composerService := NewMockComposer(t)
	composerService.EXPECT().GetDependencyPatches(anyCtx, "/tmp").Return(nil, nil).Maybe()
	composerService.EXPECT().IsPackageInstalled(anyCtx, "/tmp", "drupal/core").Return(true, nil)
	composerService.EXPECT().CheckIfPatchApplies(anyCtx, "/tmp", "drupal/core", "8.8.0", patchURL).Return(true, nil).Maybe()
	composerService.EXPECT().CheckIfPatchesApply(anyCtx, "/tmp", "drupal/core", "8.8.0", mock.Anything).Return(true, nil).Maybe()

	drupalOrgService := NewMockDrupalOrg(t)
	drupalOrgService.EXPECT().FindIssueNumber("Issue #123456").Return("123456", true)
	drupalOrgService.EXPECT().GetIssue(anyCtx, "123456").Return(fixedIssue(), nil)

	updater := &ComposerPatches1{
		logger:    zap.NewNop(),
		composer:  composerService,
		drupalOrg: drupalOrgService,
		gitlab:    nil,
	}

	patches := map[string]map[string]string{"drupal/core": {"Issue #123456": patchURL}}
	_, newPatches := updater.updatePatches(t.Context(), "/tmp", NewMockWorktree(t), upgradeCore(), patches)

	assert.NotEmpty(t, newPatches["drupal/core"])
}
