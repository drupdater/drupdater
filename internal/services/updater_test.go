package services

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	internal "github.com/drupdater/drupdater/internal"
	"github.com/drupdater/drupdater/pkg/composer"
	"github.com/drupdater/drupdater/pkg/drupalorg"
	"github.com/drupdater/drupdater/pkg/drush"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	gitlab "gitlab.com/gitlab-org/api/client-go"
	"go.uber.org/zap"
)

func TestUpdateDependencies(t *testing.T) {

	logger := zap.NewNop()
	t.Run("Update without patches and plugins", func(t *testing.T) {

		composerService := composer.NewMockRunner(t)
		drupalOrgService := drupalorg.NewMockClient(t)
		worktree := internal.NewMockWorktree(t)

		composerService.On("GetConfig", mock.Anything, "/tmp", "extra.patches").Return("", assert.AnError)
		composerService.On("GetAllowPlugins", mock.Anything, "/tmp").Return(map[string]bool{}, nil)
		composerService.On("SetConfig", mock.Anything, "/tmp", "allow-plugins", "true").Return(nil)
		composerService.On("Update", mock.Anything, "/tmp", []string{}, []string{}, false, false).Return("", nil)
		composerService.On("Normalize", mock.Anything, "/tmp").Return("", nil)
		composerService.On("SetAllowPlugins", mock.Anything, "/tmp", map[string]bool{}).Return(nil)
		composerService.On("GetAllowPlugins", mock.Anything, "/tmp").Return(map[string]bool{}, nil)

		composerService.On("GetInstalledPlugins", mock.Anything, "/tmp").Return(map[string]interface{}{}, nil)
		composerService.On("ListPendingUpdates", mock.Anything, "/tmp", []string{}, false).Return([]composer.PackageChange{}, nil)

		worktree.On("AddGlob", "composer.*").Return(nil)
		worktree.On("Commit", "Update composer.json and composer.lock", &git.CommitOptions{}).Return(plumbing.NewHash(""), nil)

		updater := &DefaultUpdater{
			logger:    logger,
			composer:  composerService,
			drupalOrg: drupalOrgService,
		}

		report, err := updater.UpdateDependencies(t.Context(), "/tmp", []string{}, worktree, false)
		assert.False(t, report.PatchUpdates.Changes())

		composerService.AssertExpectations(t)
		drupalOrgService.AssertExpectations(t)
		worktree.AssertExpectations(t)

		assert.NoError(t, err)
	})
}

func TestExportConfiguration(t *testing.T) {

	worktree := internal.NewMockWorktree(t)
	worktree.On("Add", "/tmp").Return(plumbing.NewHash(""), nil)
	worktree.On("Commit", "Update configuration site1", &git.CommitOptions{}).Return(plumbing.NewHash(""), nil)

	settingsService := NewMockSettingsService(t)

	repositoryService := NewMockRepositoryService(t)
	repositoryService.On("IsSomethingStagedInPath", worktree, "/tmp").Return(true, nil)

	drush := drush.NewMockRunner(t)
	drush.On("ExportConfiguration", mock.Anything, "/tmp", "site1").Return(nil)
	drush.On("GetConfigSyncDir", mock.Anything, "/tmp", "site1", true).Return("/tmp", nil)

	logger := zap.NewNop()

	updater := &DefaultUpdater{
		logger:     logger,
		drush:      drush,
		settings:   settingsService,
		repository: repositoryService,
	}

	err := updater.ExportConfiguration(t.Context(), worktree, "/tmp", "site1")
	if err != nil {
		t.Fatalf("Failed to export configuration: %v", err)
	}

}

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
		updater := &DefaultUpdater{
			logger: logger,

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
		updater := &DefaultUpdater{
			logger: logger,

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

		updater := &DefaultUpdater{
			logger: logger,

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

		updater := &DefaultUpdater{
			logger: logger,

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

		updater := &DefaultUpdater{
			logger: logger,

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

		updater := &DefaultUpdater{
			logger: logger,

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

		updater := &DefaultUpdater{
			logger: logger,

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

		updater := &DefaultUpdater{
			logger: logger,

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

		updater := &DefaultUpdater{
			logger: logger,

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

func TestUpdateDrupal(t *testing.T) {

	logger := zap.NewNop()

	t.Run("Update drupal", func(t *testing.T) {

		worktree := internal.NewMockWorktree(t)
		settingsService := NewMockSettingsService(t)
		repositoryService := NewMockRepositoryService(t)
		drushService := drush.NewMockRunner(t)

		repositoryService.On("IsSomethingStagedInPath", worktree, "/tmp/config").Return(false, nil)

		settingsService.On("ConfigureDatabase", mock.Anything, "/tmp", "site1").Return(nil)
		settingsService.On("ConfigureDatabase", mock.Anything, "/tmp", "site2").Return(nil)

		drushService.On("GetUpdateHooks", mock.Anything, "/tmp", "site1").Return(map[string]drush.UpdateHook{}, nil)
		drushService.On("GetUpdateHooks", mock.Anything, "/tmp", "site2").Return(map[string]drush.UpdateHook{
			"pre-update": {
				Module:      "module",
				UpdateID:    1,
				Description: "description",
				Type:        "type",
			},
		}, nil)
		drushService.On("UpdateSite", mock.Anything, "/tmp", "site1").Return(nil)
		drushService.On("UpdateSite", mock.Anything, "/tmp", "site2").Return(nil)
		drushService.On("ConfigResave", mock.Anything, "/tmp", "site1").Return(nil)
		drushService.On("ConfigResave", mock.Anything, "/tmp", "site2").Return(nil)
		drushService.On("ExportConfiguration", mock.Anything, "/tmp", "site1").Return(nil)
		drushService.On("ExportConfiguration", mock.Anything, "/tmp", "site2").Return(nil)
		drushService.On("GetConfigSyncDir", mock.Anything, "/tmp", "site1", true).Return("/tmp/config", nil)
		drushService.On("GetConfigSyncDir", mock.Anything, "/tmp", "site2", true).Return("/tmp/config", nil)

		worktree.On("Add", "/tmp/config").Return(plumbing.NewHash(""), nil)

		updater := &DefaultUpdater{
			logger: logger,

			settings:   settingsService,
			repository: repositoryService,
			drush:      drushService,
		}

		result, err := updater.UpdateDrupal(t.Context(), "/tmp", worktree, []string{"site1", "site2"})

		assert.Equal(t, UpdateHooksPerSite{
			"site2": map[string]drush.UpdateHook{
				"pre-update": {
					Module:      "module",
					UpdateID:    1,
					Description: "description",
					Type:        "type",
				},
			},
		}, result)

		settingsService.AssertExpectations(t)
		worktree.AssertExpectations(t)

		assert.NoError(t, err)
	})

}
