package services

import (
	"os"
	"testing"

	"drupdater/internal"
	"drupdater/internal/codehosting"
	"drupdater/internal/utils"

	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.uber.org/zap"
)

func TestStartUpdate(t *testing.T) {
	logger := zap.NewNop()
	installer := NewMockInstallerService(t)
	updater := NewMockUpdaterService(t)
	repositoryService := NewMockRepositoryService(t)
	vcsProviderFactory := codehosting.NewMockVcsProviderFactory(t)
	vcsProvider := codehosting.NewMockPlatform(t)
	repository := internal.NewMockRepository(t)
	commandExecutor := utils.NewMockCommandExecutor(t)
	config := internal.Config{
		RepositoryURL: "https://example.com/repo.git",
		Branch:        "main",
		Token:         "token",
		Sites:         []string{"site1", "site2"},
		DryRun:        false,
	}

	afterUpdateCommand := NewMockAfterUpdate(t)
	afterUpdateCommand.On("Execute", "/tmp", mock.Anything).Return(nil)

	afterUpdate := []AfterUpdate{
		afterUpdateCommand,
	}

	workflowService := newWorkflowDependencyUpdateService(afterUpdate, logger, installer, updater, repositoryService, vcsProviderFactory, config, commandExecutor)

	worktree := internal.NewMockWorktree(t)
	worktree.On("Checkout", mock.Anything).Return(nil)

	installer.On("InstallDrupal", config.RepositoryURL, config.Branch, config.Token, config.Sites).Return(nil)
	repositoryService.On("CloneRepository", config.RepositoryURL, config.Branch, config.Token).Return(repository, worktree, "/tmp", nil)
	repositoryService.On("GetHeadCommit", repository).Return(&object.Commit{}, nil)
	updater.On("UpdateDependencies", "/tmp", []string{}, mock.Anything, false).Return(DependencyUpdateReport{
		PatchUpdates: PatchUpdates{
			Removed: []RemovedPatch{
				{
					Package:          "package1",
					PatchPath:        "patch1",
					Reason:           "reason1",
					PatchDescription: "package1 not installed anymore",
				},
				{
					Package:          "package1",
					PatchPath:        "patch1",
					Reason:           "Fixed",
					PatchDescription: "Issue #issue1: [title1](link1) was fixed in version 2.0",
				},
			},
			Updated: []UpdatedPatch{
				{
					Package:           "package2",
					PreviousPatchPath: "oldPatch",
					NewPatchPath:      "newPatch",
					PatchDescription:  "description",
				},
			},
			Conflicts: []ConflictPatch{
				{
					Package:          "package3",
					PatchPath:        "patch3",
					FixedVersion:     "2.0",
					NewVersion:       "3.0",
					PatchDescription: "description",
				},
			},
		},
		AddedAllowPlugins: []string{"plugin1", "plugin2"},
	}, nil)
	updater.On("UpdateDrupal", "/tmp", mock.Anything, config.Sites).Return(UpdateHooksPerSite{
		"site1": map[string]UpdateHook{
			"hook": {
				Module:      "module",
				UpdateID:    1,
				Description: "description",
				Type:        "type",
			},
		},
	}, nil)
	vcsProviderFactory.On("Create", "https://example.com/repo.git", "token").Return(vcsProvider)

	fixture, _ := os.ReadFile("testdata/dependency_update.md")
	vcsProvider.On("CreateMergeRequest", mock.Anything, string(fixture), mock.Anything, config.Branch).Return(nil)
	repository.On("Push", mock.Anything).Return(nil)
	commandExecutor.On("GenerateDiffTable", mock.Anything, mock.Anything, true).Return("Dummy Table", nil)

	err := workflowService.StartUpdate()

	assert.NoError(t, err)
	installer.AssertExpectations(t)
	repositoryService.AssertExpectations(t)
	updater.AssertExpectations(t)
	vcsProviderFactory.AssertExpectations(t)
	vcsProvider.AssertExpectations(t)
}

func TestStartUpdateWithDryRun(t *testing.T) {
	logger := zap.NewNop()
	installer := NewMockInstallerService(t)
	updater := NewMockUpdaterService(t)
	repositoryService := NewMockRepositoryService(t)
	vcsProviderFactory := codehosting.NewMockVcsProviderFactory(t)
	vcsProvider := codehosting.NewMockPlatform(t)
	repository := internal.NewMockRepository(t)
	commandExecutor := utils.NewMockCommandExecutor(t)
	config := internal.Config{
		RepositoryURL: "https://example.com/repo.git",
		Branch:        "main",
		Token:         "token",
		Sites:         []string{"site1", "site2"},
		DryRun:        true,
	}

	afterUpdateCommand := NewMockAfterUpdate(t)
	afterUpdateCommand.On("Execute", "/tmp", mock.Anything).Return(nil)

	afterUpdate := []AfterUpdate{
		afterUpdateCommand,
	}

	workflowService := newWorkflowDependencyUpdateService(afterUpdate, logger, installer, updater, repositoryService, vcsProviderFactory, config, commandExecutor)

	worktree := internal.NewMockWorktree(t)
	worktree.On("Checkout", mock.Anything).Return(nil)

	installer.On("InstallDrupal", config.RepositoryURL, config.Branch, config.Token, config.Sites).Return(nil)
	repositoryService.On("CloneRepository", config.RepositoryURL, config.Branch, config.Token).Return(repository, worktree, "/tmp", nil)
	repositoryService.On("GetHeadCommit", repository).Return(&object.Commit{}, nil)
	updater.On("UpdateDependencies", "/tmp", []string{}, mock.Anything, false).Return(DependencyUpdateReport{}, nil)
	updater.On("UpdateDrupal", "/tmp", mock.Anything, config.Sites).Return(UpdateHooksPerSite{}, nil)
	commandExecutor.On("GenerateDiffTable", mock.Anything, mock.Anything, true).Return("Dummy Table", nil)

	err := workflowService.StartUpdate()

	assert.NoError(t, err)
	installer.AssertExpectations(t)
	repositoryService.AssertExpectations(t)
	updater.AssertExpectations(t)
	vcsProviderFactory.AssertExpectations(t)
	vcsProvider.AssertExpectations(t)
}
