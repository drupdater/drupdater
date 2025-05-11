package services

import (
	"context"
	"os"
	"testing"

	"github.com/drupdater/drupdater/internal"
	"github.com/drupdater/drupdater/internal/codehosting"
	"github.com/drupdater/drupdater/pkg/composer"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.uber.org/zap"
)

func TestStartUpdate(t *testing.T) {
	// Setup
	logger := zap.NewNop()
	installer := NewMockInstaller(t)
	repositoryService := NewMockRepository(t)
	vcsProvider := NewMockPlatform(t)
	repository := NewMockGitRepository(t)
	mockComposer := NewMockComposer(t)
	drush := NewMockDrush(t)
	ctx := context.Background()

	config := internal.Config{
		RepositoryURL: "https://example.com/repo.git",
		Branch:        "main",
		Token:         "token",
		Sites:         []string{"site1"},
		DryRun:        false,
	}

	// Configure mock expectations
	worktree := NewMockWorktree(t)
	worktree.EXPECT().Commit(mock.Anything, mock.Anything).Return(plumbing.NewHash(""), nil)
	worktree.EXPECT().AddGlob(mock.Anything).Return(nil)
	worktree.EXPECT().Checkout(mock.Anything).Return(nil)

	installer.EXPECT().Install(mock.Anything, "/tmp", "site1").Return(nil)
	installer.EXPECT().ConfigureDatabase(mock.Anything, "/tmp", "site1").Return(nil)

	drush.EXPECT().UpdateSite(mock.Anything, "/tmp", "site1").Return(nil)
	drush.EXPECT().ExportConfiguration(mock.Anything, "/tmp", "site1").Return(nil)
	drush.EXPECT().ConfigResave(mock.Anything, "/tmp", "site1").Return(nil)

	repositoryService.EXPECT().CloneRepository(config.RepositoryURL, config.Branch, config.Token, "user", "mail").Return(repository, worktree, "/tmp", nil).Times(2)
	repositoryService.EXPECT().BranchExists(repository, mock.Anything).Return(false, nil)

	repository.EXPECT().Push(mock.Anything).Return(nil)

	fixture, err := os.ReadFile("testdata/dependency_update.md")
	assert.NoError(t, err, "Failed to read test fixture")

	vcsProvider.EXPECT().GetUser().Return("user", "mail")
	vcsProvider.EXPECT().CreateMergeRequest(mock.Anything, string(fixture), mock.Anything, config.Branch).Return(codehosting.MergeRequest{}, nil)

	mockComposer.EXPECT().Update(mock.Anything, "/tmp", mock.Anything, mock.Anything, false, false).Return([]composer.PackageChange{
		{
			Package: "drupal/core",
			From:    "9.0.0",
			To:      "9.1.0",
		},
	}, nil)
	mockComposer.EXPECT().Install(mock.Anything, "/tmp").Return(nil)
	mockComposer.EXPECT().GetLockHash("/tmp").Return("dummy-hash", nil)

	// Execute
	workflowService := NewWorkflowBaseService(logger, config, drush, vcsProvider, repositoryService, installer, mockComposer)
	err = workflowService.StartUpdate(ctx, nil)

	// Assert
	assert.NoError(t, err)
	installer.AssertExpectations(t)
	repositoryService.AssertExpectations(t)
	repository.AssertExpectations(t)
	drush.AssertExpectations(t)
	mockComposer.AssertExpectations(t)
	vcsProvider.AssertExpectations(t)
}

func TestStartUpdateWithDryRun(t *testing.T) {
	// Setup
	logger := zap.NewNop()
	installer := NewMockInstaller(t)
	repositoryService := NewMockRepository(t)
	vcsProvider := NewMockPlatform(t)
	repository := NewMockGitRepository(t)
	mockComposer := NewMockComposer(t)
	drush := NewMockDrush(t)
	ctx := context.Background()

	config := internal.Config{
		RepositoryURL: "https://example.com/repo.git",
		Branch:        "main",
		Token:         "token",
		Sites:         []string{"site1"},
		DryRun:        true,
	}

	// Configure mock expectations
	worktree := NewMockWorktree(t)
	worktree.EXPECT().Commit(mock.Anything, mock.Anything).Return(plumbing.NewHash(""), nil)
	worktree.EXPECT().AddGlob(mock.Anything).Return(nil)
	worktree.EXPECT().Checkout(mock.Anything).Return(nil)

	installer.EXPECT().Install(mock.Anything, "/tmp", "site1").Return(nil)
	installer.EXPECT().ConfigureDatabase(mock.Anything, "/tmp", "site1").Return(nil)

	drush.EXPECT().UpdateSite(mock.Anything, "/tmp", "site1").Return(nil)
	drush.EXPECT().ExportConfiguration(mock.Anything, "/tmp", "site1").Return(nil)
	drush.EXPECT().ConfigResave(mock.Anything, "/tmp", "site1").Return(nil)

	repositoryService.EXPECT().CloneRepository(config.RepositoryURL, config.Branch, config.Token, "user", "mail").Return(repository, worktree, "/tmp", nil).Times(2)
	repositoryService.EXPECT().BranchExists(repository, mock.Anything).Return(false, nil)

	vcsProvider.EXPECT().GetUser().Return("user", "mail")

	mockComposer.EXPECT().Update(mock.Anything, "/tmp", mock.Anything, mock.Anything, false, false).Return([]composer.PackageChange{
		{
			Package: "drupal/core",
			From:    "9.0.0",
			To:      "9.1.0",
		},
	}, nil)
	mockComposer.EXPECT().Install(mock.Anything, "/tmp").Return(nil)
	mockComposer.EXPECT().GetLockHash("/tmp").Return("dummy-hash", nil)

	// Execute
	workflowService := NewWorkflowBaseService(logger, config, drush, vcsProvider, repositoryService, installer, mockComposer)
	err := workflowService.StartUpdate(ctx, nil)

	// Assert
	assert.NoError(t, err)
	installer.AssertExpectations(t)
	repositoryService.AssertExpectations(t)
	repository.AssertExpectations(t)
	drush.AssertExpectations(t)
	mockComposer.AssertExpectations(t)
	vcsProvider.AssertExpectations(t)
}
