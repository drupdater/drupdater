package services

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/drupdater/drupdater/internal"
	"github.com/drupdater/drupdater/internal/codehosting"
	"github.com/drupdater/drupdater/pkg/composer"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/gookit/event"
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
	workflowService := NewWorkflowBaseService(logger, config, drush, vcsProvider, repositoryService, installer, mockComposer, event.NewManager(""))
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

func TestStartUpdateNoChanges(t *testing.T) {
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

	// installCode runs concurrently with updateSharedCode and always completes.
	// installSite may or may not run depending on goroutine scheduling after cancel.
	worktree := NewMockWorktree(t)

	vcsProvider.EXPECT().GetUser().Return("user", "mail")

	// installCode: one CloneRepository + Install
	// updateSharedCode: one CloneRepository + Update (returns empty → AbortError)
	repositoryService.EXPECT().CloneRepository(config.RepositoryURL, config.Branch, config.Token, "user", "mail").Return(repository, worktree, "/tmp", nil).Times(2)

	mockComposer.EXPECT().Install(mock.Anything, "/tmp").Return(nil)
	mockComposer.EXPECT().Update(mock.Anything, "/tmp", mock.Anything, mock.Anything, false, false).Return([]composer.PackageChange{}, nil)

	// installSite and updateSite may not run if ctx is cancelled in time.
	installer.EXPECT().Install(mock.Anything, "/tmp", "site1").Return(nil).Maybe()
	installer.EXPECT().ConfigureDatabase(mock.Anything, "/tmp", "site1").Return(nil).Maybe()
	drush.EXPECT().UpdateSite(mock.Anything, "/tmp", "site1").Return(nil).Maybe()
	drush.EXPECT().ExportConfiguration(mock.Anything, "/tmp", "site1").Return(nil).Maybe()
	drush.EXPECT().ConfigResave(mock.Anything, "/tmp", "site1").Return(nil).Maybe()

	// Execute
	workflowService := NewWorkflowBaseService(logger, config, drush, vcsProvider, repositoryService, installer, mockComposer, event.NewManager(""))
	err := workflowService.StartUpdate(ctx, nil)

	// Assert: should get an AbortError, not a nil or other error
	var abortErr AbortError
	assert.ErrorAs(t, err, &abortErr)
	installer.AssertExpectations(t)
	repositoryService.AssertExpectations(t)
	repository.AssertExpectations(t)
	drush.AssertExpectations(t)
	mockComposer.AssertExpectations(t)
	vcsProvider.AssertExpectations(t)
}

func TestStartUpdateBranchAlreadyExists(t *testing.T) {
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

	worktree := NewMockWorktree(t)
	worktree.EXPECT().Commit(mock.Anything, mock.Anything).Return(plumbing.NewHash(""), nil)
	worktree.EXPECT().AddGlob(mock.Anything).Return(nil)

	vcsProvider.EXPECT().GetUser().Return("user", "mail")

	repositoryService.EXPECT().CloneRepository(config.RepositoryURL, config.Branch, config.Token, "user", "mail").Return(repository, worktree, "/tmp", nil).Times(2)
	repositoryService.EXPECT().BranchExists(repository, mock.Anything).Return(true, nil)

	mockComposer.EXPECT().Install(mock.Anything, "/tmp").Return(nil)
	mockComposer.EXPECT().Update(mock.Anything, "/tmp", mock.Anything, mock.Anything, false, false).Return([]composer.PackageChange{
		{
			Package: "drupal/core",
			From:    "9.0.0",
			To:      "9.1.0",
		},
	}, nil)
	mockComposer.EXPECT().GetLockHash("/tmp").Return("dummy-hash", nil)

	installer.EXPECT().Install(mock.Anything, "/tmp", "site1").Return(nil).Maybe()
	installer.EXPECT().ConfigureDatabase(mock.Anything, "/tmp", "site1").Return(nil).Maybe()
	drush.EXPECT().UpdateSite(mock.Anything, "/tmp", "site1").Return(nil).Maybe()
	drush.EXPECT().ExportConfiguration(mock.Anything, "/tmp", "site1").Return(nil).Maybe()
	drush.EXPECT().ConfigResave(mock.Anything, "/tmp", "site1").Return(nil).Maybe()

	// Execute
	workflowService := NewWorkflowBaseService(logger, config, drush, vcsProvider, repositoryService, installer, mockComposer, event.NewManager(""))
	err := workflowService.StartUpdate(ctx, nil)

	// Assert: should get an AbortError, not a nil or other error
	var abortErr AbortError
	assert.ErrorAs(t, err, &abortErr)
	assert.Contains(t, err.Error(), "already exists")
	repositoryService.AssertExpectations(t)
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
	workflowService := NewWorkflowBaseService(logger, config, drush, vcsProvider, repositoryService, installer, mockComposer, event.NewManager(""))
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

func TestStartUpdateFireEventError(t *testing.T) {
	// Verify that a FireEvent error is propagated out of StartUpdate.
	logger := zap.NewNop()
	installer := NewMockInstaller(t)
	repositoryService := NewMockRepository(t)
	vcsProvider := NewMockPlatform(t)
	repository := NewMockGitRepository(t)
	mockComposer := NewMockComposer(t)
	drush := NewMockDrush(t)
	dispatcher := NewMockEventDispatcher(t)
	ctx := context.Background()

	config := internal.Config{
		RepositoryURL: "https://example.com/repo.git",
		Branch:        "main",
		Token:         "token",
		Sites:         []string{"site1"},
		DryRun:        false,
	}

	worktree := NewMockWorktree(t)

	vcsProvider.EXPECT().GetUser().Return("user", "mail")

	// installCode and updateSharedCode each clone the repository.
	repositoryService.EXPECT().CloneRepository(config.RepositoryURL, config.Branch, config.Token, "user", "mail").
		Return(repository, worktree, "/tmp", nil).Times(2)

	mockComposer.EXPECT().Install(mock.Anything, "/tmp").Return(nil)

	// The dispatcher returns an error on the first FireEvent call (PreComposerUpdateEvent).
	fireErr := errors.New("event bus unavailable")
	dispatcher.EXPECT().FireEvent(mock.Anything).Return(fireErr)

	// installSite may or may not run before the context is cancelled.
	installer.EXPECT().Install(mock.Anything, "/tmp", "site1").Return(nil).Maybe()
	installer.EXPECT().ConfigureDatabase(mock.Anything, "/tmp", "site1").Return(nil).Maybe()
	drush.EXPECT().UpdateSite(mock.Anything, "/tmp", "site1").Return(nil).Maybe()
	drush.EXPECT().ExportConfiguration(mock.Anything, "/tmp", "site1").Return(nil).Maybe()
	drush.EXPECT().ConfigResave(mock.Anything, "/tmp", "site1").Return(nil).Maybe()

	workflowService := NewWorkflowBaseService(logger, config, drush, vcsProvider, repositoryService, installer, mockComposer, dispatcher)
	err := workflowService.StartUpdate(ctx, nil)

	assert.ErrorContains(t, err, "failed to fire event")
	assert.ErrorContains(t, err, "event bus unavailable")
}
