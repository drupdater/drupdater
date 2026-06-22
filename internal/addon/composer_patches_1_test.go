package addon

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/drupdater/drupdater/pkg/composer"
	"github.com/drupdater/drupdater/pkg/drupalorg"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/gookit/event"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	gitlab "gitlab.com/gitlab-org/api/client-go"
	"go.uber.org/zap"
)

func TestComposerPatches1_SubscribedEvents(t *testing.T) {
	h := &ComposerPatches1{}
	events := h.SubscribedEvents()

	assert.Contains(t, events, "pre-composer-update")
	item := events["pre-composer-update"].(event.ListenerItem)
	assert.Equal(t, event.Normal, item.Priority)
}

func TestComposerPatches1_RenderTemplate_NoChanges(t *testing.T) {
	logger := zap.NewNop()
	h := &ComposerPatches1{logger: logger, patchUpdates: PatchUpdates{}}
	result, err := h.RenderTemplate()
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestUpdatePatches(t *testing.T) {

	logger := zap.NewNop()
	t.Setenv("DRUPALCODE_ACCESS_TOKEN", "test")

	t.Run("Local patch still applies", func(t *testing.T) {

		composerService := NewMockComposer(t)
		composerService.EXPECT().GetDependencyPatches(mock.Anything, "/tmp").Return(nil, nil).Maybe()
		drupalOrgService := NewMockDrupalOrg(t)

		worktree := NewMockWorktree(t)

		composerService.EXPECT().IsPackageInstalled(mock.Anything, "/tmp", "drupal/core").Return(true, nil)

		drupalOrgService.EXPECT().FindIssueNumber("local patch without issue number").Return("", false)
		drupalOrgService.EXPECT().FindIssueNumber("patches/core/0001-local-patch.patch").Return("", false)

		composerService.EXPECT().CheckIfPatchApplies(mock.Anything, "drupal/core", "8.8.0", "/tmp/patches/core/0001-local-patch.patch").Return(true, nil)
		updater := &ComposerPatches1{
			logger:    logger,
			composer:  composerService,
			drupalOrg: drupalOrgService,
		}

		operations := []composer.PackageChange{
			{
				Action:  "Upgrade",
				Package: "drupal/core",
				To:      "8.8.0",
			},
		}
		patches := map[string]map[string]string{
			"drupal/core": {
				"local patch without issue number": "patches/core/0001-local-patch.patch",
			},
		}

		report, newPatches := updater.updatePatches(t.Context(), "/tmp", worktree, operations, patches)
		assert.Equal(t, patches, newPatches)
		assert.False(t, report.Changes())

		composerService.AssertExpectations(t)
		drupalOrgService.AssertExpectations(t)
	})

	t.Run("Local patch is not deduplicated against dependencies", func(t *testing.T) {
		// A local (relative) path that happens to match a dependency string must NOT be
		// removed: local paths are package-relative, so they are not the same file.
		composerService := NewMockComposer(t)
		drupalOrgService := NewMockDrupalOrg(t)
		worktree := NewMockWorktree(t)

		composerService.EXPECT().IsPackageInstalled(mock.Anything, "/tmp", "drupal/core").Return(true, nil)
		composerService.EXPECT().GetDependencyPatches(mock.Anything, "/tmp").Return(map[string]map[string]bool{
			"drupal/core": {"patches/local.patch": true},
		}, nil)
		drupalOrgService.EXPECT().FindIssueNumber(mock.Anything).Return("", false)
		composerService.EXPECT().CheckIfPatchApplies(mock.Anything, "drupal/core", "8.8.0", "/tmp/patches/local.patch").Return(true, nil)

		updater := &ComposerPatches1{logger: logger, composer: composerService, drupalOrg: drupalOrgService}
		operations := []composer.PackageChange{{Action: "Upgrade", Package: "drupal/core", To: "8.8.0"}}
		patches := map[string]map[string]string{"drupal/core": {"local": "patches/local.patch"}}

		report, newPatches := updater.updatePatches(t.Context(), "/tmp", worktree, operations, patches)
		assert.Equal(t, "patches/local.patch", newPatches["drupal/core"]["local"])
		assert.Empty(t, report.Removed)
	})

	t.Run("GetDependencyPatches error is non-fatal", func(t *testing.T) {
		composerService := NewMockComposer(t)
		drupalOrgService := NewMockDrupalOrg(t)
		worktree := NewMockWorktree(t)

		composerService.EXPECT().IsPackageInstalled(mock.Anything, "/tmp", "drupal/core").Return(true, nil)
		composerService.EXPECT().GetDependencyPatches(mock.Anything, "/tmp").Return(nil, assert.AnError)
		drupalOrgService.EXPECT().FindIssueNumber(mock.Anything).Return("", false)
		composerService.EXPECT().CheckIfPatchApplies(mock.Anything, "drupal/core", "8.8.0", "/tmp/patches/local.patch").Return(true, nil)

		updater := &ComposerPatches1{logger: logger, composer: composerService, drupalOrg: drupalOrgService}
		operations := []composer.PackageChange{{Action: "Upgrade", Package: "drupal/core", To: "8.8.0"}}
		patches := map[string]map[string]string{"drupal/core": {"local": "patches/local.patch"}}

		report, newPatches := updater.updatePatches(t.Context(), "/tmp", worktree, operations, patches)
		assert.Equal(t, "patches/local.patch", newPatches["drupal/core"]["local"])
		assert.False(t, report.Changes())
	})

	t.Run("Multiple patches apply together", func(t *testing.T) {
		composerService := NewMockComposer(t)
		drupalOrgService := NewMockDrupalOrg(t)
		worktree := NewMockWorktree(t)

		composerService.EXPECT().IsPackageInstalled(mock.Anything, "/tmp", "drupal/core").Return(true, nil)
		composerService.EXPECT().GetDependencyPatches(mock.Anything, "/tmp").Return(nil, nil)
		drupalOrgService.EXPECT().FindIssueNumber(mock.Anything).Return("", false)
		composerService.EXPECT().CheckIfPatchApplies(mock.Anything, "drupal/core", "8.8.0", "/tmp/patches/a.patch").Return(true, nil)
		composerService.EXPECT().CheckIfPatchApplies(mock.Anything, "drupal/core", "8.8.0", "/tmp/patches/b.patch").Return(true, nil)
		composerService.EXPECT().CheckIfPatchesApply(mock.Anything, "drupal/core", "8.8.0", mock.Anything).Return(true, nil)

		updater := &ComposerPatches1{logger: logger, composer: composerService, drupalOrg: drupalOrgService}
		operations := []composer.PackageChange{{Action: "Upgrade", Package: "drupal/core", To: "8.8.0"}}
		patches := map[string]map[string]string{"drupal/core": {"a": "patches/a.patch", "b": "patches/b.patch"}}

		report, _ := updater.updatePatches(t.Context(), "/tmp", worktree, operations, patches)
		assert.False(t, report.Changes())
	})

	t.Run("Combined patch check error is non-fatal", func(t *testing.T) {
		composerService := NewMockComposer(t)
		drupalOrgService := NewMockDrupalOrg(t)
		worktree := NewMockWorktree(t)

		composerService.EXPECT().IsPackageInstalled(mock.Anything, "/tmp", "drupal/core").Return(true, nil)
		composerService.EXPECT().GetDependencyPatches(mock.Anything, "/tmp").Return(nil, nil)
		drupalOrgService.EXPECT().FindIssueNumber(mock.Anything).Return("", false)
		composerService.EXPECT().CheckIfPatchApplies(mock.Anything, "drupal/core", "8.8.0", "/tmp/patches/a.patch").Return(true, nil)
		composerService.EXPECT().CheckIfPatchApplies(mock.Anything, "drupal/core", "8.8.0", "/tmp/patches/b.patch").Return(true, nil)
		composerService.EXPECT().CheckIfPatchesApply(mock.Anything, "drupal/core", "8.8.0", mock.Anything).Return(false, assert.AnError)

		updater := &ComposerPatches1{logger: logger, composer: composerService, drupalOrg: drupalOrgService}
		operations := []composer.PackageChange{{Action: "Upgrade", Package: "drupal/core", To: "8.8.0"}}
		patches := map[string]map[string]string{"drupal/core": {"a": "patches/a.patch", "b": "patches/b.patch"}}

		report, _ := updater.updatePatches(t.Context(), "/tmp", worktree, operations, patches)
		assert.Empty(t, report.Conflicts)
	})

	t.Run("Patch already provided by a dependency is removed", func(t *testing.T) {
		composerService := NewMockComposer(t)
		drupalOrgService := NewMockDrupalOrg(t)
		worktree := NewMockWorktree(t)

		const depPatch = "https://www.drupal.org/files/issues/2024-07-16/2869592-disabled-update-module-71.patch"

		composerService.EXPECT().IsPackageInstalled(mock.Anything, "/tmp", "drupal/core").Return(true, nil)
		composerService.EXPECT().GetDependencyPatches(mock.Anything, "/tmp").Return(map[string]map[string]bool{
			"drupal/core": {depPatch: true},
		}, nil)

		updater := &ComposerPatches1{
			logger:    logger,
			composer:  composerService,
			drupalOrg: drupalOrgService,
		}

		operations := []composer.PackageChange{
			{Action: "Upgrade", Package: "drupal/core", From: "10.5.0", To: "10.6.0"},
		}
		patches := map[string]map[string]string{
			"drupal/core": {
				"Issue #2869592: [Disabled update module](https://www.drupal.org/node/2869592)": depPatch,
			},
		}

		report, newPatches := updater.updatePatches(t.Context(), "/tmp", worktree, operations, patches)
		assert.Empty(t, newPatches["drupal/core"], "patch provided by a dependency should be removed from root")
		assert.Len(t, report.Removed, 1)
		assert.Equal(t, depPatch, report.Removed[0].PatchPath)
		assert.True(t, report.Changes())

		composerService.AssertExpectations(t)
		drupalOrgService.AssertExpectations(t)
	})

	t.Run("Local patch not applies", func(t *testing.T) {

		composerService := NewMockComposer(t)
		composerService.EXPECT().GetDependencyPatches(mock.Anything, "/tmp").Return(nil, nil).Maybe()
		drupalOrgService := NewMockDrupalOrg(t)

		worktree := NewMockWorktree(t)

		drupalOrgService.EXPECT().FindIssueNumber("local patch without issue number").Return("", false)
		drupalOrgService.EXPECT().FindIssueNumber("patches/core/0001-local-patch.patch").Return("", false)

		composerService.EXPECT().CheckIfPatchApplies(mock.Anything, "drupal/core", "8.8.0", "/tmp/patches/core/0001-local-patch.patch").Return(false, nil)
		composerService.EXPECT().IsPackageInstalled(mock.Anything, "/tmp", "drupal/core").Return(true, nil)
		updater := &ComposerPatches1{
			logger:    logger,
			composer:  composerService,
			drupalOrg: drupalOrgService,
		}

		operations := []composer.PackageChange{
			{
				Action:  "Upgrade",
				Package: "drupal/core",
				From:    "8.7.0",
				To:      "8.8.0",
			},
		}
		patches := map[string]map[string]string{
			"drupal/core": {
				"local patch without issue number": "patches/core/0001-local-patch.patch",
			},
		}

		report, newPatches := updater.updatePatches(t.Context(), "/tmp", worktree, operations, patches)
		assert.Equal(t, map[string]map[string]string{
			"drupal/core": {
				"local patch without issue number": "patches/core/0001-local-patch.patch",
			},
		}, newPatches)
		assert.Equal(t, PatchUpdates{
			Conflicts: []ConflictPatch{
				{
					Package:          "drupal/core",
					PatchPath:        "patches/core/0001-local-patch.patch",
					FixedVersion:     "8.7.0",
					NewVersion:       "8.8.0",
					PatchDescription: "local patch without issue number",
				},
			},
		}, report)
		assert.True(t, report.Changes())

		composerService.AssertExpectations(t)
		drupalOrgService.AssertExpectations(t)
	})

	t.Run("Remote patch still applies", func(t *testing.T) {

		composerService := NewMockComposer(t)
		composerService.EXPECT().GetDependencyPatches(mock.Anything, "/tmp").Return(nil, nil).Maybe()
		drupalOrgService := NewMockDrupalOrg(t)

		worktree := NewMockWorktree(t)

		composerService.EXPECT().IsPackageInstalled(mock.Anything, "/tmp", "drupal/core").Return(true, nil)

		drupalOrgService.EXPECT().FindIssueNumber("Issue #123456 \"With problems\"").Return("123456", true)
		drupalOrgService.EXPECT().GetIssue(mock.Anything, "123456").Return(&drupalorg.Issue{
			ID:     "123456",
			Title:  "Alot of problems",
			Status: "1",
			URL:    "https://www.drupal.org/node/123456",
		}, nil)

		composerService.EXPECT().CheckIfPatchApplies(mock.Anything, "drupal/core", "8.8.0", "/tmp/patches/remote/0001-remote.patch").Return(true, nil)

		updater := &ComposerPatches1{
			logger:    logger,
			composer:  composerService,
			drupalOrg: drupalOrgService,
		}

		operations := []composer.PackageChange{
			{
				Action:  "Upgrade",
				Package: "drupal/core",
				From:    "8.7.0",
				To:      "8.8.0",
			},
		}
		patches := map[string]map[string]string{
			"drupal/core": {
				"Issue #123456 \"With problems\"": "patches/remote/0001-remote.patch",
			},
		}

		report, newPatches := updater.updatePatches(t.Context(), "/tmp", worktree, operations, patches)
		assert.Equal(t, map[string]map[string]string{
			"drupal/core": {
				"Issue #123456: [Alot of problems](https://www.drupal.org/node/123456)": "patches/remote/0001-remote.patch",
			},
		}, newPatches)
		assert.False(t, report.Changes())

		composerService.AssertExpectations(t)
		drupalOrgService.AssertExpectations(t)
	})

	t.Run("Current patch fails, remote patch still applies", func(t *testing.T) {

		composerService := NewMockComposer(t)
		composerService.EXPECT().GetDependencyPatches(mock.Anything, "/tmp").Return(nil, nil).Maybe()
		drupalOrgService := NewMockDrupalOrg(t)

		composerService.EXPECT().IsPackageInstalled(mock.Anything, "/tmp", "drupal/core").Return(true, nil)

		worktree := NewMockWorktree(t)
		worktree.EXPECT().Add("patches/drupal/123456-111111-alot_of_problems.diff").Return(plumbing.NewHash(""), nil)
		worktree.EXPECT().Remove("patches/remote/0001-remote.patch").Return(plumbing.NewHash(""), nil)

		drupalOrgService.EXPECT().FindIssueNumber("Issue #123456 \"With problems\"").Return("123456", true)
		drupalOrgService.EXPECT().GetIssue(mock.Anything, "123456").Return(&drupalorg.Issue{
			ID:     "123456",
			Title:  "Alot of problems",
			Status: "1",
			URL:    "https://www.drupal.org/node/123456",
			Project: struct {
				MaschineName string `json:"machine_name"`
			}{
				MaschineName: "drupal",
			},
		}, nil)

		composerService.EXPECT().CheckIfPatchApplies(mock.Anything, "drupal/core", "8.8.0", "/tmp/patches/remote/0001-remote.patch").Return(false, nil)
		composerService.EXPECT().CheckIfPatchApplies(mock.Anything, "drupal/core", "8.8.0", "/tmp/patches/drupal/123456-111111-alot_of_problems.diff").Return(true, nil)

		var serverURL string
		mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)

			var jsonString []byte
			if r.URL.Path == "/api/v4/projects/issue/drupal-123456" {
				response := &gitlab.Project{
					ID: 5678,
				}
				jsonString, _ = json.Marshal(response)
			}
			if r.URL.Path == "/api/v4/projects/project/drupal/merge_requests" {
				response := []gitlab.MergeRequest{
					{
						BasicMergeRequest: gitlab.BasicMergeRequest{
							ID:     1234,
							IID:    5678,
							Title:  "Remote patch",
							SHA:    "111111",
							WebURL: serverURL + "/project/drupal/-/merge_requests/1",
						},
					},
				}
				jsonString, _ = json.Marshal(response)
			}
			if r.URL.Path == "/project/drupal/-/merge_requests/1.diff" {
				w.Header().Set("Content-Type", "text/plain")
				_, _ = w.Write([]byte("patch content"))
				return
			}

			_, err := w.Write(jsonString)
			assert.NoError(t, err)
		}))
		serverURL = mockServer.URL
		defer mockServer.Close()

		gitClient, _ := gitlab.NewClient("", gitlab.WithBaseURL(mockServer.URL))

		updater := &ComposerPatches1{
			logger:     logger,
			composer:   composerService,
			drupalOrg:  drupalOrgService,
			gitlab:     gitClient,
			httpClient: mockServer.Client(),
		}

		operations := []composer.PackageChange{
			{
				Action:  "Upgrade",
				Package: "drupal/core",
				From:    "8.7.0",
				To:      "8.8.0",
			},
		}
		patches := map[string]map[string]string{
			"drupal/core": {
				"Issue #123456 \"With problems\"": "patches/remote/0001-remote.patch",
			},
		}

		report, newPatches := updater.updatePatches(t.Context(), "/tmp", worktree, operations, patches)
		assert.Equal(t, map[string]map[string]string{
			"drupal/core": {
				"Issue #123456: [Alot of problems](https://www.drupal.org/node/123456)": "patches/drupal/123456-111111-alot_of_problems.diff",
			},
		}, newPatches)
		assert.True(t, report.Changes())

		composerService.AssertExpectations(t)
		drupalOrgService.AssertExpectations(t)
	})

	t.Run("Current patch fails, remote patch also fails", func(t *testing.T) {

		composerService := NewMockComposer(t)
		composerService.EXPECT().GetDependencyPatches(mock.Anything, "/tmp").Return(nil, nil).Maybe()
		drupalOrgService := NewMockDrupalOrg(t)

		worktree := NewMockWorktree(t)

		composerService.EXPECT().IsPackageInstalled(mock.Anything, "/tmp", "drupal/core").Return(true, nil)

		drupalOrgService.EXPECT().FindIssueNumber("Issue #123456 \"With problems\"").Return("123456", true)
		drupalOrgService.EXPECT().GetIssue(mock.Anything, "123456").Return(&drupalorg.Issue{
			ID:     "123456",
			Title:  "Alot of problems",
			Status: "1",
			URL:    "https://www.drupal.org/node/123456",
			Project: struct {
				MaschineName string `json:"machine_name"`
			}{
				MaschineName: "drupal",
			},
		}, nil)

		composerService.EXPECT().CheckIfPatchApplies(mock.Anything, "drupal/core", "8.8.0", "/tmp/patches/remote/0001-remote.patch").Return(false, nil)
		composerService.EXPECT().CheckIfPatchApplies(mock.Anything, "drupal/core", "8.8.0", "/tmp/patches/drupal/123456-111111-alot_of_problems.diff").Return(false, nil)

		var serverURL string
		mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)

			var jsonString []byte
			if r.URL.Path == "/api/v4/projects/issue/drupal-123456" {
				response := &gitlab.Project{
					ID: 5678,
				}
				jsonString, _ = json.Marshal(response)
			}
			if r.URL.Path == "/api/v4/projects/project/drupal/merge_requests" {
				response := []gitlab.MergeRequest{
					{
						BasicMergeRequest: gitlab.BasicMergeRequest{
							ID:     1234,
							IID:    5678,
							Title:  "Remote patch",
							SHA:    "111111",
							WebURL: serverURL + "/project/drupal/-/merge_requests/1",
						},
					},
				}
				jsonString, _ = json.Marshal(response)
			}
			if r.URL.Path == "/project/drupal/-/merge_requests/1.diff" {
				w.Header().Set("Content-Type", "text/plain")
				_, _ = w.Write([]byte("patch content"))
				return
			}

			_, err := w.Write(jsonString)
			assert.NoError(t, err)
		}))
		serverURL = mockServer.URL
		defer mockServer.Close()

		gitClient, _ := gitlab.NewClient("", gitlab.WithBaseURL(mockServer.URL))

		updater := &ComposerPatches1{
			logger:     logger,
			composer:   composerService,
			drupalOrg:  drupalOrgService,
			gitlab:     gitClient,
			httpClient: mockServer.Client(),
		}

		operations := []composer.PackageChange{
			{
				Action:  "Upgrade",
				Package: "drupal/core",
				From:    "8.7.0",
				To:      "8.8.0",
			},
		}
		patches := map[string]map[string]string{
			"drupal/core": {
				"Issue #123456 \"With problems\"": "patches/remote/0001-remote.patch",
			},
		}

		report, newPatches := updater.updatePatches(t.Context(), "/tmp", worktree, operations, patches)
		assert.Equal(t, map[string]map[string]string{
			"drupal/core": {
				"Issue #123456: [Alot of problems](https://www.drupal.org/node/123456)": "patches/remote/0001-remote.patch",
			},
		}, newPatches)
		assert.Equal(t, PatchUpdates{
			Conflicts: []ConflictPatch{
				{
					Package:          "drupal/core",
					PatchPath:        "patches/remote/0001-remote.patch",
					FixedVersion:     "8.7.0",
					NewVersion:       "8.8.0",
					PatchDescription: "Issue #123456: [Alot of problems](https://www.drupal.org/node/123456)",
				},
			},
		}, report)
		assert.True(t, report.Changes())

		composerService.AssertExpectations(t)
		drupalOrgService.AssertExpectations(t)
	})

	t.Run("Remote patch was committed and released", func(t *testing.T) {

		composerService := NewMockComposer(t)
		composerService.EXPECT().GetDependencyPatches(mock.Anything, "/tmp").Return(nil, nil).Maybe()
		drupalOrgService := NewMockDrupalOrg(t)

		worktree := NewMockWorktree(t)
		worktree.EXPECT().Remove("patches/remote/0001-remote.patch").Return(plumbing.NewHash(""), nil)

		composerService.EXPECT().IsPackageInstalled(mock.Anything, "/tmp", "drupal/core").Return(true, nil)

		drupalOrgService.EXPECT().FindIssueNumber("Issue #123456 \"With problems\"").Return("123456", true)
		drupalOrgService.EXPECT().GetIssue(mock.Anything, "123456").Return(&drupalorg.Issue{
			ID:     "123456",
			Title:  "Alot of problems",
			Status: "7",
			URL:    "https://www.drupal.org/node/123456",
			Project: struct {
				MaschineName string `json:"machine_name"`
			}{
				MaschineName: "drupal",
			},
		}, nil)

		mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)

			var jsonString []byte
			if r.URL.Path == "/api/v4/projects/project/drupal/-/search" {
				response := []gitlab.Commit{
					{
						ID: "5678",
					}}
				jsonString, _ = json.Marshal(response)
			}

			_, err := w.Write(jsonString)
			assert.NoError(t, err)
		}))
		defer mockServer.Close()

		git, _ := gitlab.NewClient("", gitlab.WithBaseURL(mockServer.URL))

		updater := &ComposerPatches1{
			logger:    logger,
			composer:  composerService,
			drupalOrg: drupalOrgService,
			gitlab:    git,
		}

		operations := []composer.PackageChange{
			{
				Action:  "Upgrade",
				Package: "drupal/core",
				From:    "8.7.0",
				To:      "8.8.0",
			},
		}
		patches := map[string]map[string]string{
			"drupal/core": {
				"Issue #123456 \"With problems\"": "patches/remote/0001-remote.patch",
			},
		}

		report, newPatches := updater.updatePatches(t.Context(), "/tmp", worktree, operations, patches)
		assert.Equal(t, map[string]map[string]string{}, newPatches)
		assert.True(t, report.Changes())

		composerService.AssertExpectations(t)
		drupalOrgService.AssertExpectations(t)
	})

	t.Run("Remote patch was committed, but not yet releases", func(t *testing.T) {

		composerService := NewMockComposer(t)
		composerService.EXPECT().GetDependencyPatches(mock.Anything, "/tmp").Return(nil, nil).Maybe()
		drupalOrgService := NewMockDrupalOrg(t)

		worktree := NewMockWorktree(t)

		composerService.EXPECT().IsPackageInstalled(mock.Anything, "/tmp", "drupal/core").Return(true, nil)

		composerService.EXPECT().CheckIfPatchApplies(mock.Anything, "drupal/core", "8.8.0", "/tmp/patches/remote/0001-remote.patch").Return(true, nil)

		drupalOrgService.EXPECT().FindIssueNumber("Issue #123456 \"With problems\"").Return("123456", true)
		drupalOrgService.EXPECT().GetIssue(mock.Anything, "123456").Return(&drupalorg.Issue{
			ID:     "123456",
			Title:  "Alot of problems",
			Status: "7",
			URL:    "https://www.drupal.org/node/123456",
			Project: struct {
				MaschineName string `json:"machine_name"`
			}{
				MaschineName: "drupal",
			},
		}, nil)

		mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)

			var jsonString []byte
			if r.URL.Path == "/api/v4/projects/project/drupal/-/search" {
				response := []gitlab.Commit{}
				jsonString, _ = json.Marshal(response)
			}

			_, err := w.Write(jsonString)
			assert.NoError(t, err)
		}))
		defer mockServer.Close()

		git, _ := gitlab.NewClient("", gitlab.WithBaseURL(mockServer.URL))

		updater := &ComposerPatches1{
			logger:    logger,
			composer:  composerService,
			drupalOrg: drupalOrgService,
			gitlab:    git,
		}

		operations := []composer.PackageChange{
			{
				Action:  "Upgrade",
				Package: "drupal/core",
				From:    "8.7.0",
				To:      "8.8.0",
			},
		}
		patches := map[string]map[string]string{
			"drupal/core": {
				"Issue #123456 \"With problems\"": "patches/remote/0001-remote.patch",
			},
		}

		report, newPatches := updater.updatePatches(t.Context(), "/tmp", worktree, operations, patches)
		assert.Equal(t, map[string]map[string]string{
			"drupal/core": {
				"Issue #123456: [Alot of problems](https://www.drupal.org/node/123456)": "patches/remote/0001-remote.patch",
			},
		}, newPatches)
		assert.False(t, report.Changes())

		composerService.AssertExpectations(t)
		drupalOrgService.AssertExpectations(t)
	})

	t.Run("Module will be removed", func(t *testing.T) {

		composerService := NewMockComposer(t)
		composerService.EXPECT().GetDependencyPatches(mock.Anything, "/tmp").Return(nil, nil).Maybe()
		drupalOrgService := NewMockDrupalOrg(t)

		worktree := NewMockWorktree(t)
		worktree.EXPECT().Remove("patches/core/0001-local-patch.patch").Return(plumbing.NewHash(""), nil)

		composerService.EXPECT().IsPackageInstalled(mock.Anything, "/tmp", "drupal/core").Return(true, nil)
		composerService.EXPECT().IsPackageInstalled(mock.Anything, "/tmp", "drupal/pathauto").Return(true, nil)

		updater := &ComposerPatches1{
			logger:    logger,
			composer:  composerService,
			drupalOrg: drupalOrgService,
		}

		operations := []composer.PackageChange{
			{
				Action:  "Remove",
				Package: "drupal/core",
			},
			{
				Action:  "Remove",
				Package: "drupal/paragraphs",
			},
		}
		patches := map[string]map[string]string{
			"drupal/core": {
				"local patch without issue number": "patches/core/0001-local-patch.patch",
				"remote patch":                     "https://www.drupal.org/node/123456.diff",
			},
			"drupal/pathauto": {
				"local patch without issue number": "patches/core/0001-local-patch.patch",
			},
		}

		report, newPatches := updater.updatePatches(t.Context(), "/tmp", worktree, operations, patches)
		assert.Equal(t, map[string]map[string]string{
			"drupal/pathauto": {
				"local patch without issue number": "patches/core/0001-local-patch.patch",
			},
		}, newPatches)
		assert.True(t, report.Changes())

		composerService.AssertExpectations(t)
		drupalOrgService.AssertExpectations(t)
	})

	t.Run("Multiple patches conflict when applied together", func(t *testing.T) {
		composerService := NewMockComposer(t)
		composerService.EXPECT().GetDependencyPatches(mock.Anything, "/tmp").Return(nil, nil).Maybe()
		drupalOrgService := NewMockDrupalOrg(t)
		worktree := NewMockWorktree(t)

		composerService.EXPECT().IsPackageInstalled(mock.Anything, "/tmp", "drupal/core").Return(true, nil)

		drupalOrgService.EXPECT().FindIssueNumber("patch one").Return("", false)
		drupalOrgService.EXPECT().FindIssueNumber("patches/core/patch1.patch").Return("", false)
		drupalOrgService.EXPECT().FindIssueNumber("patch two").Return("", false)
		drupalOrgService.EXPECT().FindIssueNumber("patches/core/patch2.patch").Return("", false)

		composerService.EXPECT().CheckIfPatchApplies(mock.Anything, "drupal/core", "8.8.0", "/tmp/patches/core/patch1.patch").Return(true, nil)
		composerService.EXPECT().CheckIfPatchApplies(mock.Anything, "drupal/core", "8.8.0", "/tmp/patches/core/patch2.patch").Return(true, nil)
		composerService.EXPECT().CheckIfPatchesApply(mock.Anything, "drupal/core", "8.8.0", mock.Anything).Return(false, nil)

		updater := &ComposerPatches1{
			logger:    logger,
			composer:  composerService,
			drupalOrg: drupalOrgService,
		}

		operations := []composer.PackageChange{
			{Action: "Upgrade", Package: "drupal/core", From: "8.7.0", To: "8.8.0"},
		}
		patches := map[string]map[string]string{
			"drupal/core": {
				"patch one": "patches/core/patch1.patch",
				"patch two": "patches/core/patch2.patch",
			},
		}

		report, _ := updater.updatePatches(t.Context(), "/tmp", worktree, operations, patches)
		assert.True(t, report.Changes())
		assert.Len(t, report.Conflicts, 1)
		assert.Equal(t, "drupal/core", report.Conflicts[0].Package)
		assert.Equal(t, "8.7.0", report.Conflicts[0].FixedVersion)
		assert.Equal(t, "8.8.0", report.Conflicts[0].NewVersion)

		composerService.AssertExpectations(t)
		drupalOrgService.AssertExpectations(t)
	})

	t.Run("Module not installed", func(t *testing.T) {
		composerService := NewMockComposer(t)
		composerService.EXPECT().GetDependencyPatches(mock.Anything, "/tmp").Return(nil, nil).Maybe()
		drupalOrgService := NewMockDrupalOrg(t)

		worktree := NewMockWorktree(t)
		worktree.EXPECT().Remove("patches/core/0001-local-patch.patch").Return(plumbing.NewHash(""), nil)

		composerService.EXPECT().IsPackageInstalled(mock.Anything, "/tmp", "drupal/core").Return(false, nil)

		updater := &ComposerPatches1{
			logger:    logger,
			composer:  composerService,
			drupalOrg: drupalOrgService,
		}

		operations := []composer.PackageChange{}
		patches := map[string]map[string]string{
			"drupal/core": {
				"local patch without issue number": "patches/core/0001-local-patch.patch",
				"remote patch":                     "https://www.drupal.org/node/123456.diff",
			},
		}

		report, newPatches := updater.updatePatches(t.Context(), "/tmp", worktree, operations, patches)
		assert.Equal(t, map[string]map[string]string{}, newPatches)
		assert.True(t, report.Changes())

		composerService.AssertExpectations(t)
		drupalOrgService.AssertExpectations(t)
	})
}

func TestComposer_Patches_1_RenderTemplate(t *testing.T) {
	// Setup
	fixture, err := os.ReadFile("testdata/composer_patches_1.md")
	require.NoError(t, err, "Failed to read test fixture")

	expected := string(fixture)
	logger := zap.NewNop()
	composerRunner := NewMockComposer(t)
	drupalorgService := NewMockDrupalOrg(t)

	// Initialize system under test
	ap := NewComposerPatches1(logger, composerRunner, drupalorgService, http.DefaultClient)
	ap.patchUpdates = PatchUpdates{
		Conflicts: []ConflictPatch{
			{
				Package:          "package3",
				PatchPath:        "patch3",
				FixedVersion:     "2.0",
				NewVersion:       "3.0",
				PatchDescription: "description",
			},
		},
		Updated: []UpdatedPatch{
			{
				Package:           "package2",
				PatchDescription:  "description",
				PreviousPatchPath: "oldPatch",
				NewPatchPath:      "newPatch",
			},
		},
		Removed: []RemovedPatch{
			{
				PatchDescription: "package1 not installed anymore",
				Package:          "package1",
				PatchPath:        "patch1",
				Reason:           "reason1",
			},
			{
				PatchDescription: "Issue #issue1: [title1](link1) was fixed in version 2.0",
				Package:          "package1",
				PatchPath:        "patch1",
				Reason:           "Fixed",
			},
		},
	}

	// Execute
	result, err := ap.RenderTemplate()

	// Verify
	require.NoError(t, err)
	assert.Equal(t, expected, result)
}

func TestDownloadFile(t *testing.T) {
	logger := zap.NewNop()

	t.Run("success", func(t *testing.T) {
		const content = "--- a/file\n+++ b/file\n@@ -1 +1 @@\n-old\n+new\n"
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, content)
		}))
		defer server.Close()

		dir := t.TempDir()
		h := &ComposerPatches1{logger: logger, httpClient: server.Client()}

		err := h.downloadFile(t.Context(), server.URL+"/patch.diff", dir, "patch.diff")
		require.NoError(t, err)

		data, err := os.ReadFile(dir + "/patch.diff")
		require.NoError(t, err)
		assert.Equal(t, content, string(data))
	})

	t.Run("http error response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		dir := t.TempDir()
		h := &ComposerPatches1{logger: logger, httpClient: server.Client()}

		err := h.downloadFile(t.Context(), server.URL+"/patch.diff", dir, "patch.diff")
		require.ErrorContains(t, err, "status code 404")
	})

	t.Run("invalid url", func(t *testing.T) {
		h := &ComposerPatches1{logger: logger, httpClient: http.DefaultClient}

		err := h.downloadFile(t.Context(), "not-a-valid-url", t.TempDir(), "patch.diff")
		require.Error(t, err)
	})

	t.Run("mock http client", func(t *testing.T) {
		const content = "patch data"
		mockClient := NewMockHTTPClient(t)
		mockClient.EXPECT().Do(mock.AnythingOfType("*http.Request")).Return(&http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(content)),
		}, nil)

		dir := t.TempDir()
		h := &ComposerPatches1{logger: logger, httpClient: mockClient}

		err := h.downloadFile(t.Context(), "http://example.com/patch.diff", dir, "patch.diff")
		require.NoError(t, err)

		data, err := os.ReadFile(dir + "/patch.diff")
		require.NoError(t, err)
		assert.Equal(t, content, string(data))
	})
}
