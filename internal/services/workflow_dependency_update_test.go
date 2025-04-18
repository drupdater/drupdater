package services

import (
	"os"
	"testing"

	"github.com/drupdater/drupdater/internal"
	"github.com/drupdater/drupdater/internal/codehosting"
	"github.com/drupdater/drupdater/pkg/composer"
	"github.com/drupdater/drupdater/pkg/drush"

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
	composer := composer.NewMockComposerService(t)

	config := internal.Config{
		RepositoryURL: "https://example.com/repo.git",
		Branch:        "main",
		Token:         "token",
		Sites:         []string{"site1", "site2"},
		DryRun:        false,
	}

	afterUpdateCommand := NewMockAfterUpdate(t)
	afterUpdateCommand.On("Execute", mock.Anything, "/tmp", mock.Anything).Return(nil)

	afterUpdate := []AfterUpdate{
		afterUpdateCommand,
	}

	strategy := NewDependencyUpdateStrategy(afterUpdate, logger, config)
	workflowService := NewWorkflowBaseService(logger, config, updater, vcsProviderFactory, repositoryService, installer, composer)

	worktree := internal.NewMockWorktree(t)
	worktree.On("Checkout", mock.Anything).Return(nil)

	installer.On("InstallDrupal", mock.Anything, config.RepositoryURL, config.Branch, config.Token, config.Sites).Return(nil)
	repositoryService.On("CloneRepository", config.RepositoryURL, config.Branch, config.Token).Return(repository, worktree, "/tmp", nil)
	repositoryService.On("BranchExists", mock.Anything, mock.Anything).Return(false, nil)
	updater.On("UpdateDependencies", mock.Anything, "/tmp", []string{}, mock.Anything, false).Return(DependencyUpdateReport{
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
	updater.On("UpdateDrupal", mock.Anything, "/tmp", mock.Anything, config.Sites).Return(UpdateHooksPerSite{
		"site1": map[string]drush.UpdateHook{
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
	vcsProvider.On("CreateMergeRequest", mock.Anything, string(fixture), mock.Anything, config.Branch).Return(codehosting.MergeRequest{}, nil)
	repository.On("Push", mock.Anything).Return(nil)
	composer.On("Diff", mock.Anything, mock.Anything, mock.Anything, true).Return("Dummy Table", nil)

	err := workflowService.StartUpdate(t.Context(), strategy)

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
	composer := composer.NewMockComposerService(t)

	config := internal.Config{
		RepositoryURL: "https://example.com/repo.git",
		Branch:        "main",
		Token:         "token",
		Sites:         []string{"site1", "site2"},
		DryRun:        true,
	}

	afterUpdateCommand := NewMockAfterUpdate(t)
	afterUpdateCommand.On("Execute", mock.Anything, "/tmp", mock.Anything).Return(nil)

	afterUpdate := []AfterUpdate{
		afterUpdateCommand,
	}

	strategy := NewDependencyUpdateStrategy(afterUpdate, logger, config)
	workflowService := NewWorkflowBaseService(logger, config, updater, vcsProviderFactory, repositoryService, installer, composer)

	worktree := internal.NewMockWorktree(t)
	worktree.On("Checkout", mock.Anything).Return(nil)

	installer.On("InstallDrupal", mock.Anything, config.RepositoryURL, config.Branch, config.Token, config.Sites).Return(nil)
	repositoryService.On("CloneRepository", config.RepositoryURL, config.Branch, config.Token).Return(repository, worktree, "/tmp", nil)
	repositoryService.On("BranchExists", mock.Anything, mock.Anything).Return(false, nil)
	updater.On("UpdateDependencies", mock.Anything, "/tmp", []string{}, mock.Anything, false).Return(DependencyUpdateReport{}, nil)
	updater.On("UpdateDrupal", mock.Anything, "/tmp", mock.Anything, config.Sites).Return(UpdateHooksPerSite{}, nil)
	composer.On("Diff", mock.Anything, mock.Anything, mock.Anything, true).Return("Dummy Table", nil)

	err := workflowService.StartUpdate(t.Context(), strategy)

	assert.NoError(t, err)
	installer.AssertExpectations(t)
	repositoryService.AssertExpectations(t)
	updater.AssertExpectations(t)
	vcsProviderFactory.AssertExpectations(t)
	vcsProvider.AssertExpectations(t)
}
