package services

import (
	"os"
	"testing"

	"github.com/drupdater/drupdater/internal"
	"github.com/drupdater/drupdater/internal/codehosting"
	"github.com/drupdater/drupdater/pkg/composer"
	"github.com/drupdater/drupdater/pkg/drupal"
	"github.com/drupdater/drupdater/pkg/drush"
	"github.com/drupdater/drupdater/pkg/repo"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.uber.org/zap"
)

func TestStartUpdate(t *testing.T) {
	logger := zap.NewNop()
	installer := drupal.NewMockInstallerService(t)
	updater := NewMockUpdaterService(t)
	repositoryService := repo.NewMockRepositoryService(t)
	vcsProvider := codehosting.NewMockPlatform(t)
	repository := internal.NewMockRepository(t)
	composer := composer.NewMockRunner(t)

	config := internal.Config{
		RepositoryURL: "https://example.com/repo.git",
		Branch:        "main",
		Token:         "token",
		Sites:         []string{"site1"},
		DryRun:        false,
	}

	strategy := NewDependencyUpdateStrategy(logger, config)
	workflowService := NewWorkflowBaseService(logger, config, updater, vcsProvider, repositoryService, installer, composer)

	worktree := internal.NewMockWorktree(t)
	worktree.On("Checkout", mock.Anything).Return(nil)

	installer.On("InstallDrupal", mock.Anything, "/tmp", "site1").Return(nil)
	//installer.On("InstallDrupal", mock.Anything, "/tmp", "site2").Return(nil)
	repositoryService.On("CloneRepository", config.RepositoryURL, config.Branch, config.Token).Return(repository, worktree, "/tmp", nil)
	repositoryService.On("BranchExists", mock.Anything, mock.Anything).Return(false, nil)
	updater.On("UpdateDependencies", mock.Anything, "/tmp", []string{}, mock.Anything, false).Return(nil)
	updater.On("UpdateDrupal", mock.Anything, "/tmp", mock.Anything, "site1").Return(map[string]drush.UpdateHook{
		"hook": {
			Module:      "module",
			UpdateID:    1,
			Description: "description",
			Type:        "type",
		},
	}, nil)

	fixture, _ := os.ReadFile("testdata/dependency_update.md")
	vcsProvider.On("CreateMergeRequest", mock.Anything, string(fixture), mock.Anything, config.Branch).Return(codehosting.MergeRequest{}, nil)
	repository.On("Push", mock.Anything).Return(nil)
	composer.On("Install", mock.Anything, "/tmp").Return(nil)
	composer.On("Diff", mock.Anything, mock.Anything, mock.Anything, true).Return("Dummy Table", nil)

	err := workflowService.StartUpdate(t.Context(), strategy, nil)

	assert.NoError(t, err)
	installer.AssertExpectations(t)
	repositoryService.AssertExpectations(t)
	updater.AssertExpectations(t)
	vcsProvider.AssertExpectations(t)
	vcsProvider.AssertExpectations(t)
}

func TestStartUpdateWithDryRun(t *testing.T) {
	logger := zap.NewNop()
	installer := drupal.NewMockInstallerService(t)
	updater := NewMockUpdaterService(t)
	repositoryService := repo.NewMockRepositoryService(t)
	vcsProvider := codehosting.NewMockPlatform(t)
	repository := internal.NewMockRepository(t)
	composer := composer.NewMockRunner(t)

	config := internal.Config{
		RepositoryURL: "https://example.com/repo.git",
		Branch:        "main",
		Token:         "token",
		Sites:         []string{"site1", "site2"},
		DryRun:        true,
	}

	strategy := NewDependencyUpdateStrategy(logger, config)
	workflowService := NewWorkflowBaseService(logger, config, updater, vcsProvider, repositoryService, installer, composer)

	worktree := internal.NewMockWorktree(t)
	worktree.On("Checkout", mock.Anything).Return(nil)

	installer.On("InstallDrupal", mock.Anything, "/tmp", "site1").Return(nil)
	installer.On("InstallDrupal", mock.Anything, "/tmp", "site2").Return(nil)
	repositoryService.On("CloneRepository", config.RepositoryURL, config.Branch, config.Token).Return(repository, worktree, "/tmp", nil)
	repositoryService.On("BranchExists", mock.Anything, mock.Anything).Return(false, nil)
	updater.On("UpdateDependencies", mock.Anything, "/tmp", []string{}, mock.Anything, false).Return(nil)
	updater.On("UpdateDrupal", mock.Anything, "/tmp", mock.Anything, "site1").Return(map[string]drush.UpdateHook{}, nil)
	updater.On("UpdateDrupal", mock.Anything, "/tmp", mock.Anything, "site2").Return(map[string]drush.UpdateHook{}, nil)
	composer.On("Diff", mock.Anything, mock.Anything, mock.Anything, true).Return("Dummy Table", nil)
	composer.On("Install", mock.Anything, "/tmp").Return(nil)

	err := workflowService.StartUpdate(t.Context(), strategy, nil)

	assert.NoError(t, err)
	installer.AssertExpectations(t)
	repositoryService.AssertExpectations(t)
	updater.AssertExpectations(t)
	vcsProvider.AssertExpectations(t)
	vcsProvider.AssertExpectations(t)
}
