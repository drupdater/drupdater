package composerpatches

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/drupdater/drupdater/internal"
	"github.com/drupdater/drupdater/pkg/composer"
	"github.com/drupdater/drupdater/pkg/drupalorg"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	gitlab "gitlab.com/gitlab-org/api/client-go"
	"go.uber.org/zap"
)

func TestUpdatePatches(t *testing.T) {

	logger := zap.NewNop()
	t.Setenv("DRUPALCODE_ACCESS_TOKEN", "test")

	t.Run("Local patch still applies", func(t *testing.T) {

		composerService := composer.NewMockRunner(t)
		drupalOrgService := drupalorg.NewMockClient(t)

		worktree := internal.NewMockWorktree(t)

		composerService.On("IsPackageInstalled", mock.Anything, "/tmp", "drupal/core").Return(true, nil)

		drupalOrgService.On("FindIssueNumber", "local patch without issue number").Return("", false)
		drupalOrgService.On("FindIssueNumber", "patches/core/0001-local-patch.patch").Return("", false)

		composerService.On("CheckIfPatchApplies", mock.Anything, "drupal/core", "8.8.0", "/tmp/patches/core/0001-local-patch.patch").Return(true, nil)
		updater := &DefaultComposerPatches{
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

		report, newPatches := updater.UpdatePatches(t.Context(), "/tmp", worktree, operations, patches)
		assert.Equal(t, patches, newPatches)
		assert.False(t, report.Changes())

		composerService.AssertExpectations(t)
		drupalOrgService.AssertExpectations(t)
	})

	t.Run("Local patch not applies", func(t *testing.T) {

		composerService := composer.NewMockRunner(t)
		drupalOrgService := drupalorg.NewMockClient(t)

		worktree := internal.NewMockWorktree(t)

		drupalOrgService.On("FindIssueNumber", "local patch without issue number").Return("", false)
		drupalOrgService.On("FindIssueNumber", "patches/core/0001-local-patch.patch").Return("", false)

		composerService.On("CheckIfPatchApplies", mock.Anything, "drupal/core", "8.8.0", "/tmp/patches/core/0001-local-patch.patch").Return(false, nil)
		composerService.On("IsPackageInstalled", mock.Anything, "/tmp", "drupal/core").Return(true, nil)
		updater := &DefaultComposerPatches{
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

		report, newPatches := updater.UpdatePatches(t.Context(), "/tmp", worktree, operations, patches)
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

		composerService := composer.NewMockRunner(t)
		drupalOrgService := drupalorg.NewMockClient(t)

		worktree := internal.NewMockWorktree(t)

		composerService.On("IsPackageInstalled", mock.Anything, "/tmp", "drupal/core").Return(true, nil)

		drupalOrgService.On("FindIssueNumber", "Issue #123456 \"With problems\"").Return("123456", true)
		drupalOrgService.On("GetIssue", "123456").Return(&drupalorg.Issue{
			ID:     "123456",
			Title:  "Alot of problems",
			Status: "1",
			URL:    "https://www.drupal.org/node/123456",
		}, nil)

		composerService.On("CheckIfPatchApplies", mock.Anything, "drupal/core", "8.8.0", "/tmp/patches/remote/0001-remote.patch").Return(true, nil)

		updater := &DefaultComposerPatches{
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

		report, newPatches := updater.UpdatePatches(t.Context(), "/tmp", worktree, operations, patches)
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

		composerService := composer.NewMockRunner(t)
		drupalOrgService := drupalorg.NewMockClient(t)

		composerService.On("IsPackageInstalled", mock.Anything, "/tmp", "drupal/core").Return(true, nil)

		worktree := internal.NewMockWorktree(t)
		worktree.On("Add", "patches/drupal/123456-111111-alot_of_problems.diff").Return(plumbing.NewHash(""), nil)
		worktree.On("Remove", "patches/remote/0001-remote.patch").Return(plumbing.NewHash(""), nil)

		drupalOrgService.On("FindIssueNumber", "Issue #123456 \"With problems\"").Return("123456", true)
		drupalOrgService.On("GetIssue", "123456").Return(&drupalorg.Issue{
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

		composerService.On("CheckIfPatchApplies", mock.Anything, "drupal/core", "8.8.0", "/tmp/patches/remote/0001-remote.patch").Return(false, nil)
		composerService.On("CheckIfPatchApplies", mock.Anything, "drupal/core", "8.8.0", "/tmp/patches/drupal/123456-111111-alot_of_problems.diff").Return(true, nil)

		mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)

			jsonString := make([]byte, 0)
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
							ID:    1234,
							IID:   5678,
							Title: "Remote patch",
							SHA:   "111111",
						},
					},
				}
				jsonString, _ = json.Marshal(response)
			}

			_, err := w.Write(jsonString)
			assert.NoError(t, err)
		}))
		defer mockServer.Close()

		git, _ := gitlab.NewClient("", gitlab.WithBaseURL(mockServer.URL))

		updater := &DefaultComposerPatches{
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

		report, newPatches := updater.UpdatePatches(t.Context(), "/tmp", worktree, operations, patches)
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

		composerService := composer.NewMockRunner(t)
		drupalOrgService := drupalorg.NewMockClient(t)

		worktree := internal.NewMockWorktree(t)

		composerService.On("IsPackageInstalled", mock.Anything, "/tmp", "drupal/core").Return(true, nil)

		drupalOrgService.On("FindIssueNumber", "Issue #123456 \"With problems\"").Return("123456", true)
		drupalOrgService.On("GetIssue", "123456").Return(&drupalorg.Issue{
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

		composerService.On("CheckIfPatchApplies", mock.Anything, "drupal/core", "8.8.0", "/tmp/patches/remote/0001-remote.patch").Return(false, nil)
		composerService.On("CheckIfPatchApplies", mock.Anything, "drupal/core", "8.8.0", "/tmp/patches/drupal/123456-111111-alot_of_problems.diff").Return(false, nil)

		mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)

			jsonString := make([]byte, 0)
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
							ID:    1234,
							IID:   5678,
							Title: "Remote patch",
							SHA:   "111111",
						},
					},
				}
				jsonString, _ = json.Marshal(response)
			}

			_, err := w.Write(jsonString)
			assert.NoError(t, err)
		}))
		defer mockServer.Close()

		git, _ := gitlab.NewClient("", gitlab.WithBaseURL(mockServer.URL))

		updater := &DefaultComposerPatches{
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

		report, newPatches := updater.UpdatePatches(t.Context(), "/tmp", worktree, operations, patches)
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

		composerService := composer.NewMockRunner(t)
		drupalOrgService := drupalorg.NewMockClient(t)

		worktree := internal.NewMockWorktree(t)
		worktree.On("Remove", "patches/remote/0001-remote.patch").Return(plumbing.NewHash(""), nil)

		composerService.On("IsPackageInstalled", mock.Anything, "/tmp", "drupal/core").Return(true, nil)

		drupalOrgService.On("FindIssueNumber", "Issue #123456 \"With problems\"").Return("123456", true)
		drupalOrgService.On("GetIssue", "123456").Return(&drupalorg.Issue{
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

			jsonString := make([]byte, 0)
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

		updater := &DefaultComposerPatches{
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

		report, newPatches := updater.UpdatePatches(t.Context(), "/tmp", worktree, operations, patches)
		assert.Equal(t, map[string]map[string]string{}, newPatches)
		assert.True(t, report.Changes())

		composerService.AssertExpectations(t)
		drupalOrgService.AssertExpectations(t)
	})

	t.Run("Remote patch was committed, but not yet releases", func(t *testing.T) {

		composerService := composer.NewMockRunner(t)
		drupalOrgService := drupalorg.NewMockClient(t)

		worktree := internal.NewMockWorktree(t)

		composerService.On("IsPackageInstalled", mock.Anything, "/tmp", "drupal/core").Return(true, nil)

		composerService.On("CheckIfPatchApplies", mock.Anything, "drupal/core", "8.8.0", "/tmp/patches/remote/0001-remote.patch").Return(true, nil)

		drupalOrgService.On("FindIssueNumber", "Issue #123456 \"With problems\"").Return("123456", true)
		drupalOrgService.On("GetIssue", "123456").Return(&drupalorg.Issue{
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

			jsonString := make([]byte, 0)
			if r.URL.Path == "/api/v4/projects/project/drupal/-/search" {
				response := []gitlab.Commit{}
				jsonString, _ = json.Marshal(response)
			}

			_, err := w.Write(jsonString)
			assert.NoError(t, err)
		}))
		defer mockServer.Close()

		git, _ := gitlab.NewClient("", gitlab.WithBaseURL(mockServer.URL))

		updater := &DefaultComposerPatches{
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

		report, newPatches := updater.UpdatePatches(t.Context(), "/tmp", worktree, operations, patches)
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

		composerService := composer.NewMockRunner(t)
		drupalOrgService := drupalorg.NewMockClient(t)

		worktree := internal.NewMockWorktree(t)
		worktree.On("Remove", "patches/core/0001-local-patch.patch").Return(plumbing.NewHash(""), nil)

		composerService.On("IsPackageInstalled", mock.Anything, "/tmp", "drupal/core").Return(true, nil)
		composerService.On("IsPackageInstalled", mock.Anything, "/tmp", "drupal/pathauto").Return(true, nil)

		updater := &DefaultComposerPatches{
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

		report, newPatches := updater.UpdatePatches(t.Context(), "/tmp", worktree, operations, patches)
		assert.Equal(t, map[string]map[string]string{
			"drupal/pathauto": {
				"local patch without issue number": "patches/core/0001-local-patch.patch",
			},
		}, newPatches)
		assert.True(t, report.Changes())

		composerService.AssertExpectations(t)
		drupalOrgService.AssertExpectations(t)
	})

	t.Run("Module not installed", func(t *testing.T) {

		composerService := composer.NewMockRunner(t)
		drupalOrgService := drupalorg.NewMockClient(t)

		worktree := internal.NewMockWorktree(t)
		worktree.On("Remove", "patches/core/0001-local-patch.patch").Return(plumbing.NewHash(""), nil)

		composerService.On("IsPackageInstalled", mock.Anything, "/tmp", "drupal/core").Return(false, nil)

		updater := &DefaultComposerPatches{
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

		report, newPatches := updater.UpdatePatches(t.Context(), "/tmp", worktree, operations, patches)
		assert.Equal(t, map[string]map[string]string{}, newPatches)
		assert.True(t, report.Changes())

		composerService.AssertExpectations(t)
		drupalOrgService.AssertExpectations(t)
	})
}
