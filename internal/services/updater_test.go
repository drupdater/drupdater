package services

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	internal "github.com/drupdater/drupdater/internal"
	"github.com/drupdater/drupdater/internal/utils"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/stretchr/testify/assert"
	gitlab "gitlab.com/gitlab-org/api/client-go"
	"go.uber.org/zap"
)

func TestUpdateDependencies(t *testing.T) {

	logger := zap.NewNop()
	t.Run("Update without patches and plugins", func(t *testing.T) {
		commandExecutor := utils.NewMockCommandExecutor(t)
		composerService := NewMockComposerService(t)
		drupalOrgService := NewMockDrupalOrgService(t)
		worktree := internal.NewMockWorktree(t)

		commandExecutor.On("GetComposerConfig", "/tmp", "extra.patches").Return("", assert.AnError)
		commandExecutor.On("GetComposerAllowPlugins", "/tmp").Return(map[string]bool{}, nil)
		commandExecutor.On("SetComposerConfig", "/tmp", "allow-plugins", "true").Return(nil)
		commandExecutor.On("UpdateDependencies", "/tmp", []string{}, []string{}, false, false).Return("", nil)
		commandExecutor.On("RunComposerNormalize", "/tmp").Return("", nil)
		commandExecutor.On("SetComposerAllowPlugins", "/tmp", map[string]bool{}).Return(nil)

		composerService.On("GetInstalledPlugins", "/tmp").Return(map[string]interface{}{}, nil)
		composerService.On("GetComposerUpdates", "/tmp", []string{}, false).Return([]PackageChange{}, nil)

		worktree.On("AddGlob", "composer.*").Return(nil)
		worktree.On("Commit", "Update composer.json and composer.lock", &git.CommitOptions{}).Return(plumbing.NewHash(""), nil)

		updater := &DefaultUpdater{
			logger:          logger,
			commandExecutor: commandExecutor,
			composer:        composerService,
			drupalOrg:       drupalOrgService,
		}

		report, err := updater.UpdateDependencies("/tmp", []string{}, worktree, false)
		assert.False(t, report.PatchUpdates.Changes())

		commandExecutor.AssertExpectations(t)
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
	commandExecutor := utils.NewMockCommandExecutor(t)
	commandExecutor.On("ExportConfiguration", "/tmp", "site1").Return(nil)
	commandExecutor.On("GetConfigSyncDir", "/tmp", "site1", true).Return("/tmp", nil)

	logger := zap.NewNop()

	updater := &DefaultUpdater{
		logger:          logger,
		commandExecutor: commandExecutor,
		settings:        settingsService,
		repository:      repositoryService,
	}

	err := updater.ExportConfiguration(worktree, "/tmp", "site1")
	if err != nil {
		t.Fatalf("Failed to export configuration: %v", err)
	}

}

func TestUpdatePatches(t *testing.T) {

	logger := zap.NewNop()
	t.Setenv("DRUPALCODE_ACCESS_TOKEN", "test")

	t.Run("Local patch still applies", func(t *testing.T) {
		commandExecutor := utils.NewMockCommandExecutor(t)
		composerService := NewMockComposerService(t)
		drupalOrgService := NewMockDrupalOrgService(t)

		worktree := internal.NewMockWorktree(t)

		commandExecutor.On("IsPackageInstalled", "/tmp", "drupal/core").Return(true, nil)

		drupalOrgService.On("FindIssueNumber", "local patch without issue number").Return("", false)
		drupalOrgService.On("FindIssueNumber", "patches/core/0001-local-patch.patch").Return("", false)

		composerService.On("CheckPatchApplies", "drupal/core", "8.8.0", "/tmp/patches/core/0001-local-patch.patch").Return(true, nil)
		updater := &DefaultUpdater{
			logger:          logger,
			commandExecutor: commandExecutor,
			composer:        composerService,
			drupalOrg:       drupalOrgService,
		}

		operations := []PackageChange{
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

		report, newPatches := updater.UpdatePatches("/tmp", worktree, operations, patches)
		assert.Equal(t, patches, newPatches)
		assert.False(t, report.Changes())

		commandExecutor.AssertExpectations(t)
		composerService.AssertExpectations(t)
		drupalOrgService.AssertExpectations(t)
	})

	t.Run("Local patch not applies", func(t *testing.T) {
		commandExecutor := utils.NewMockCommandExecutor(t)
		composerService := NewMockComposerService(t)
		drupalOrgService := NewMockDrupalOrgService(t)

		worktree := internal.NewMockWorktree(t)

		drupalOrgService.On("FindIssueNumber", "local patch without issue number").Return("", false)
		drupalOrgService.On("FindIssueNumber", "patches/core/0001-local-patch.patch").Return("", false)

		composerService.On("CheckPatchApplies", "drupal/core", "8.8.0", "/tmp/patches/core/0001-local-patch.patch").Return(false, nil)
		commandExecutor.On("IsPackageInstalled", "/tmp", "drupal/core").Return(true, nil)
		updater := &DefaultUpdater{
			logger:          logger,
			commandExecutor: commandExecutor,
			composer:        composerService,
			drupalOrg:       drupalOrgService,
		}

		operations := []PackageChange{
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

		report, newPatches := updater.UpdatePatches("/tmp", worktree, operations, patches)
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

		commandExecutor.AssertExpectations(t)
		composerService.AssertExpectations(t)
		drupalOrgService.AssertExpectations(t)
	})

	t.Run("Remote patch still applies", func(t *testing.T) {
		commandExecutor := utils.NewMockCommandExecutor(t)
		composerService := NewMockComposerService(t)
		drupalOrgService := NewMockDrupalOrgService(t)

		worktree := internal.NewMockWorktree(t)

		commandExecutor.On("IsPackageInstalled", "/tmp", "drupal/core").Return(true, nil)

		drupalOrgService.On("FindIssueNumber", "Issue #123456 \"With problems\"").Return("123456", true)
		drupalOrgService.On("GetIssue", "123456").Return(&Issue{
			ID:     "123456",
			Title:  "Alot of problems",
			Status: "1",
			URL:    "https://www.drupal.org/node/123456",
		}, nil)

		composerService.On("CheckPatchApplies", "drupal/core", "8.8.0", "/tmp/patches/remote/0001-remote.patch").Return(true, nil)

		updater := &DefaultUpdater{
			logger:          logger,
			commandExecutor: commandExecutor,
			composer:        composerService,
			drupalOrg:       drupalOrgService,
		}

		operations := []PackageChange{
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

		report, newPatches := updater.UpdatePatches("/tmp", worktree, operations, patches)
		assert.Equal(t, map[string]map[string]string{
			"drupal/core": {
				"Issue #123456: [Alot of problems](https://www.drupal.org/node/123456)": "patches/remote/0001-remote.patch",
			},
		}, newPatches)
		assert.False(t, report.Changes())

		commandExecutor.AssertExpectations(t)
		composerService.AssertExpectations(t)
		drupalOrgService.AssertExpectations(t)
	})

	t.Run("Current patch fails, remote patch still applies", func(t *testing.T) {
		commandExecutor := utils.NewMockCommandExecutor(t)
		composerService := NewMockComposerService(t)
		drupalOrgService := NewMockDrupalOrgService(t)

		commandExecutor.On("IsPackageInstalled", "/tmp", "drupal/core").Return(true, nil)

		worktree := internal.NewMockWorktree(t)
		worktree.On("Add", "patches/drupal/123456-111111-alot_of_problems.diff").Return(plumbing.NewHash(""), nil)
		worktree.On("Remove", "patches/remote/0001-remote.patch").Return(plumbing.NewHash(""), nil)

		drupalOrgService.On("FindIssueNumber", "Issue #123456 \"With problems\"").Return("123456", true)
		drupalOrgService.On("GetIssue", "123456").Return(&Issue{
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

		composerService.On("CheckPatchApplies", "drupal/core", "8.8.0", "/tmp/patches/remote/0001-remote.patch").Return(false, nil)
		composerService.On("CheckPatchApplies", "drupal/core", "8.8.0", "/tmp/patches/drupal/123456-111111-alot_of_problems.diff").Return(true, nil)

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
			logger:          logger,
			commandExecutor: commandExecutor,
			composer:        composerService,
			drupalOrg:       drupalOrgService,
			gitlab:          git,
		}

		operations := []PackageChange{
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

		report, newPatches := updater.UpdatePatches("/tmp", worktree, operations, patches)
		assert.Equal(t, map[string]map[string]string{
			"drupal/core": {
				"Issue #123456: [Alot of problems](https://www.drupal.org/node/123456)": "patches/drupal/123456-111111-alot_of_problems.diff",
			},
		}, newPatches)
		assert.True(t, report.Changes())

		commandExecutor.AssertExpectations(t)
		composerService.AssertExpectations(t)
		drupalOrgService.AssertExpectations(t)
	})

	t.Run("Current patch fails, remote patch also fails", func(t *testing.T) {
		commandExecutor := utils.NewMockCommandExecutor(t)
		composerService := NewMockComposerService(t)
		drupalOrgService := NewMockDrupalOrgService(t)

		worktree := internal.NewMockWorktree(t)

		commandExecutor.On("IsPackageInstalled", "/tmp", "drupal/core").Return(true, nil)

		drupalOrgService.On("FindIssueNumber", "Issue #123456 \"With problems\"").Return("123456", true)
		drupalOrgService.On("GetIssue", "123456").Return(&Issue{
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

		composerService.On("CheckPatchApplies", "drupal/core", "8.8.0", "/tmp/patches/remote/0001-remote.patch").Return(false, nil)
		composerService.On("CheckPatchApplies", "drupal/core", "8.8.0", "/tmp/patches/drupal/123456-111111-alot_of_problems.diff").Return(false, nil)

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
			logger:          logger,
			commandExecutor: commandExecutor,
			composer:        composerService,
			drupalOrg:       drupalOrgService,
			gitlab:          git,
		}

		operations := []PackageChange{
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

		report, newPatches := updater.UpdatePatches("/tmp", worktree, operations, patches)
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

		commandExecutor.AssertExpectations(t)
		composerService.AssertExpectations(t)
		drupalOrgService.AssertExpectations(t)
	})

	t.Run("Remote patch was committed and released", func(t *testing.T) {
		commandExecutor := utils.NewMockCommandExecutor(t)
		composerService := NewMockComposerService(t)
		drupalOrgService := NewMockDrupalOrgService(t)

		worktree := internal.NewMockWorktree(t)
		worktree.On("Remove", "patches/remote/0001-remote.patch").Return(plumbing.NewHash(""), nil)

		commandExecutor.On("IsPackageInstalled", "/tmp", "drupal/core").Return(true, nil)

		drupalOrgService.On("FindIssueNumber", "Issue #123456 \"With problems\"").Return("123456", true)
		drupalOrgService.On("GetIssue", "123456").Return(&Issue{
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
			logger:          logger,
			commandExecutor: commandExecutor,
			composer:        composerService,
			drupalOrg:       drupalOrgService,
			gitlab:          git,
		}

		operations := []PackageChange{
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

		report, newPatches := updater.UpdatePatches("/tmp", worktree, operations, patches)
		assert.Equal(t, map[string]map[string]string{}, newPatches)
		assert.True(t, report.Changes())

		commandExecutor.AssertExpectations(t)
		composerService.AssertExpectations(t)
		drupalOrgService.AssertExpectations(t)
	})

	t.Run("Remote patch was committed, but not yet releases", func(t *testing.T) {
		commandExecutor := utils.NewMockCommandExecutor(t)
		composerService := NewMockComposerService(t)
		drupalOrgService := NewMockDrupalOrgService(t)

		worktree := internal.NewMockWorktree(t)

		commandExecutor.On("IsPackageInstalled", "/tmp", "drupal/core").Return(true, nil)

		composerService.On("CheckPatchApplies", "drupal/core", "8.8.0", "/tmp/patches/remote/0001-remote.patch").Return(true, nil)

		drupalOrgService.On("FindIssueNumber", "Issue #123456 \"With problems\"").Return("123456", true)
		drupalOrgService.On("GetIssue", "123456").Return(&Issue{
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
			logger:          logger,
			commandExecutor: commandExecutor,
			composer:        composerService,
			drupalOrg:       drupalOrgService,
			gitlab:          git,
		}

		operations := []PackageChange{
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

		report, newPatches := updater.UpdatePatches("/tmp", worktree, operations, patches)
		assert.Equal(t, map[string]map[string]string{
			"drupal/core": {
				"Issue #123456: [Alot of problems](https://www.drupal.org/node/123456)": "patches/remote/0001-remote.patch",
			},
		}, newPatches)
		assert.False(t, report.Changes())

		commandExecutor.AssertExpectations(t)
		composerService.AssertExpectations(t)
		drupalOrgService.AssertExpectations(t)
	})

	t.Run("Module will be removed", func(t *testing.T) {
		commandExecutor := utils.NewMockCommandExecutor(t)
		composerService := NewMockComposerService(t)
		drupalOrgService := NewMockDrupalOrgService(t)

		worktree := internal.NewMockWorktree(t)
		worktree.On("Remove", "patches/core/0001-local-patch.patch").Return(plumbing.NewHash(""), nil)

		commandExecutor.On("IsPackageInstalled", "/tmp", "drupal/core").Return(true, nil)
		commandExecutor.On("IsPackageInstalled", "/tmp", "drupal/pathauto").Return(true, nil)

		updater := &DefaultUpdater{
			logger:          logger,
			commandExecutor: commandExecutor,
			composer:        composerService,
			drupalOrg:       drupalOrgService,
		}

		operations := []PackageChange{
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

		report, newPatches := updater.UpdatePatches("/tmp", worktree, operations, patches)
		assert.Equal(t, map[string]map[string]string{
			"drupal/pathauto": {
				"local patch without issue number": "patches/core/0001-local-patch.patch",
			},
		}, newPatches)
		assert.True(t, report.Changes())

		commandExecutor.AssertExpectations(t)
		composerService.AssertExpectations(t)
		drupalOrgService.AssertExpectations(t)
	})

	t.Run("Module not installed", func(t *testing.T) {
		commandExecutor := utils.NewMockCommandExecutor(t)
		composerService := NewMockComposerService(t)
		drupalOrgService := NewMockDrupalOrgService(t)

		worktree := internal.NewMockWorktree(t)
		worktree.On("Remove", "patches/core/0001-local-patch.patch").Return(plumbing.NewHash(""), nil)

		commandExecutor.On("IsPackageInstalled", "/tmp", "drupal/core").Return(false, nil)

		updater := &DefaultUpdater{
			logger:          logger,
			commandExecutor: commandExecutor,
			composer:        composerService,
			drupalOrg:       drupalOrgService,
		}

		operations := []PackageChange{}
		patches := map[string]map[string]string{
			"drupal/core": {
				"local patch without issue number": "patches/core/0001-local-patch.patch",
				"remote patch":                     "https://www.drupal.org/node/123456.diff",
			},
		}

		report, newPatches := updater.UpdatePatches("/tmp", worktree, operations, patches)
		assert.Equal(t, map[string]map[string]string{}, newPatches)
		assert.True(t, report.Changes())

		commandExecutor.AssertExpectations(t)
		composerService.AssertExpectations(t)
		drupalOrgService.AssertExpectations(t)
	})
}

func TestUpdateDrupal(t *testing.T) {

	logger := zap.NewNop()

	t.Run("Update drupal", func(t *testing.T) {
		commandExecutor := utils.NewMockCommandExecutor(t)
		worktree := internal.NewMockWorktree(t)
		settingsService := NewMockSettingsService(t)
		repositoryService := NewMockRepositoryService(t)
		drushService := NewMockDrushService(t)

		repositoryService.On("IsSomethingStagedInPath", worktree, "/tmp/config").Return(false, nil)

		settingsService.On("ConfigureDatabase", "/tmp", "site1").Return(nil)
		settingsService.On("ConfigureDatabase", "/tmp", "site2").Return(nil)

		drushService.On("GetUpdateHooks", "/tmp", "site1").Return(map[string]UpdateHook{}, nil)
		drushService.On("GetUpdateHooks", "/tmp", "site2").Return(map[string]UpdateHook{
			"pre-update": {
				Module:      "module",
				UpdateID:    1,
				Description: "description",
				Type:        "type",
			},
		}, nil)
		commandExecutor.On("UpdateSite", "/tmp", "site1").Return(nil)
		commandExecutor.On("UpdateSite", "/tmp", "site2").Return(nil)
		commandExecutor.On("ConfigResave", "/tmp", "site1").Return(nil)
		commandExecutor.On("ConfigResave", "/tmp", "site2").Return(nil)
		commandExecutor.On("ExportConfiguration", "/tmp", "site1").Return(nil)
		commandExecutor.On("ExportConfiguration", "/tmp", "site2").Return(nil)
		commandExecutor.On("GetConfigSyncDir", "/tmp", "site1", true).Return("/tmp/config", nil)
		commandExecutor.On("GetConfigSyncDir", "/tmp", "site2", true).Return("/tmp/config", nil)

		worktree.On("Add", "/tmp/config").Return(plumbing.NewHash(""), nil)

		updater := &DefaultUpdater{
			logger:          logger,
			commandExecutor: commandExecutor,
			settings:        settingsService,
			repository:      repositoryService,
			drush:           drushService,
		}

		result, err := updater.UpdateDrupal("/tmp", worktree, []string{"site1", "site2"})

		assert.Equal(t, UpdateHooksPerSite{
			"site2": map[string]UpdateHook{
				"pre-update": {
					Module:      "module",
					UpdateID:    1,
					Description: "description",
					Type:        "type",
				},
			},
		}, result)

		commandExecutor.AssertExpectations(t)
		settingsService.AssertExpectations(t)
		worktree.AssertExpectations(t)

		assert.NoError(t, err)
	})

}
