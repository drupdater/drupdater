package services

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

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
	mockComposer.EXPECT().CheckPlatformReqs(mock.Anything, "/tmp").Return("", nil)
	repositoryService.EXPECT().BranchExists(repository, mock.Anything).Return(false, nil)

	repository.EXPECT().Push(mock.Anything).Return(nil)

	fixture, err := os.ReadFile("testdata/dependency_update.md")
	assert.NoError(t, err, "Failed to read test fixture")

	vcsProvider.EXPECT().GetUser(mock.Anything).Return("user", "mail")
	vcsProvider.EXPECT().CreateMergeRequest(mock.Anything, mock.Anything, string(fixture), mock.Anything, config.Branch).Return(codehosting.MergeRequest{}, nil)

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

func TestStartUpdatePublishUsesLiveContext(t *testing.T) {
	// Regression: publishWork must run on the outer (timeout-bounded) context, not the
	// errgroup-derived context. The errgroup context is cancelled as soon as g.Wait()
	// returns, so if publishWork received it, CreateMergeRequest would see a cancelled
	// context. Assert the context handed to CreateMergeRequest is still alive.
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
	worktree.EXPECT().Checkout(mock.Anything).Return(nil)

	installer.EXPECT().Install(mock.Anything, "/tmp", "site1").Return(nil)
	installer.EXPECT().ConfigureDatabase(mock.Anything, "/tmp", "site1").Return(nil)

	drush.EXPECT().UpdateSite(mock.Anything, "/tmp", "site1").Return(nil)
	drush.EXPECT().ExportConfiguration(mock.Anything, "/tmp", "site1").Return(nil)
	drush.EXPECT().ConfigResave(mock.Anything, "/tmp", "site1").Return(nil)

	repositoryService.EXPECT().CloneRepository(config.RepositoryURL, config.Branch, config.Token, "user", "mail").Return(repository, worktree, "/tmp", nil).Times(2)
	mockComposer.EXPECT().CheckPlatformReqs(mock.Anything, "/tmp").Return("", nil)
	repositoryService.EXPECT().BranchExists(repository, mock.Anything).Return(false, nil)

	repository.EXPECT().Push(mock.Anything).Return(nil)

	fixture, err := os.ReadFile("testdata/dependency_update.md")
	assert.NoError(t, err, "Failed to read test fixture")

	vcsProvider.EXPECT().GetUser(mock.Anything).Return("user", "mail")

	// Capture the context handed to publishWork's MR creation and assert it is not cancelled.
	var publishCtxErr error
	vcsProvider.EXPECT().CreateMergeRequest(mock.Anything, mock.Anything, string(fixture), mock.Anything, config.Branch).
		Run(func(ctx context.Context, _ string, _ string, _ string, _ string) {
			publishCtxErr = ctx.Err()
		}).Return(codehosting.MergeRequest{}, nil)

	mockComposer.EXPECT().Update(mock.Anything, "/tmp", mock.Anything, mock.Anything, false, false).Return([]composer.PackageChange{
		{Package: "drupal/core", From: "9.0.0", To: "9.1.0"},
	}, nil)
	mockComposer.EXPECT().Install(mock.Anything, "/tmp").Return(nil)
	mockComposer.EXPECT().GetLockHash("/tmp").Return("dummy-hash", nil)

	workflowService := NewWorkflowBaseService(logger, config, drush, vcsProvider, repositoryService, installer, mockComposer, event.NewManager(""))
	err = workflowService.StartUpdate(ctx, nil)

	assert.NoError(t, err)
	assert.NoError(t, publishCtxErr, "publishWork must receive a live (non-cancelled) context")
	vcsProvider.AssertExpectations(t)
}

func TestStartUpdateSiteFailureDoesNotPublish(t *testing.T) {
	// A failing site update must propagate the error and never push a branch or open an MR.
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

	// updateSharedCode completes fully (site updates only start once it's done).
	worktree := NewMockWorktree(t)
	worktree.EXPECT().Commit(mock.Anything, mock.Anything).Return(plumbing.NewHash(""), nil)
	worktree.EXPECT().AddGlob(mock.Anything).Return(nil)
	worktree.EXPECT().Checkout(mock.Anything).Return(nil)

	repositoryService.EXPECT().CloneRepository(config.RepositoryURL, config.Branch, config.Token, "user", "mail").Return(repository, worktree, "/tmp", nil).Times(2)
	mockComposer.EXPECT().CheckPlatformReqs(mock.Anything, "/tmp").Return("", nil)
	repositoryService.EXPECT().BranchExists(repository, mock.Anything).Return(false, nil)

	vcsProvider.EXPECT().GetUser(mock.Anything).Return("user", "mail")

	mockComposer.EXPECT().Install(mock.Anything, "/tmp").Return(nil)
	mockComposer.EXPECT().Update(mock.Anything, "/tmp", mock.Anything, mock.Anything, false, false).Return([]composer.PackageChange{
		{Package: "drupal/core", From: "9.0.0", To: "9.1.0"},
	}, nil)
	mockComposer.EXPECT().GetLockHash("/tmp").Return("dummy-hash", nil)

	installer.EXPECT().Install(mock.Anything, "/tmp", "site1").Return(nil)
	installer.EXPECT().ConfigureDatabase(mock.Anything, "/tmp", "site1").Return(nil)

	// The site update fails. ConfigResave/ExportConfiguration are left unexpected on
	// purpose: if the failure didn't short-circuit, those calls would fail the test.
	updateErr := errors.New("drush updatedb crashed")
	drush.EXPECT().UpdateSite(mock.Anything, "/tmp", "site1").Return(updateErr)

	// Execute
	workflowService := NewWorkflowBaseService(logger, config, drush, vcsProvider, repositoryService, installer, mockComposer, event.NewManager(""))
	err := workflowService.StartUpdate(ctx, nil)

	// Assert: the error surfaces and no MR was published.
	assert.ErrorIs(t, err, updateErr)
	repository.AssertNotCalled(t, "Push", mock.Anything)
	vcsProvider.AssertNotCalled(t, "CreateMergeRequest", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestStartUpdateTimeout(t *testing.T) {
	// A run-level timeout must cancel the workflow context and surface as a
	// deadline error rather than hanging.
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
		Timeout:       20 * time.Millisecond,
	}

	worktree := NewMockWorktree(t)
	vcsProvider.EXPECT().GetUser(mock.Anything).Return("user", "mail")
	repositoryService.EXPECT().CloneRepository(config.RepositoryURL, config.Branch, config.Token, "user", "mail").Return(repository, worktree, "/tmp", nil).Times(2)
	mockComposer.EXPECT().CheckPlatformReqs(mock.Anything, "/tmp").Return("", nil)

	// Both phases block on the context, simulating a wedged subprocess; the run
	// timeout is what releases them.
	mockComposer.EXPECT().Install(mock.Anything, "/tmp").RunAndReturn(func(ctx context.Context, _ string) error {
		<-ctx.Done()
		return ctx.Err()
	})
	mockComposer.EXPECT().Update(mock.Anything, "/tmp", mock.Anything, mock.Anything, false, false).RunAndReturn(func(ctx context.Context, _ string, _ []string, _ []string, _ bool, _ bool) ([]composer.PackageChange, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}).Maybe()

	// Execute
	workflowService := NewWorkflowBaseService(logger, config, drush, vcsProvider, repositoryService, installer, mockComposer, event.NewManager(""))
	err := workflowService.StartUpdate(ctx, nil)

	// Assert: the deadline propagates and nothing is published.
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	repository.AssertNotCalled(t, "Push", mock.Anything)
	vcsProvider.AssertNotCalled(t, "CreateMergeRequest", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestStartUpdatePlatformReqsFail(t *testing.T) {
	// If the runtime PHP does not satisfy the project's platform requirements, the run
	// aborts before composer update and nothing is published.
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
	}

	worktree := NewMockWorktree(t)
	vcsProvider.EXPECT().GetUser(mock.Anything).Return("user", "mail")
	repositoryService.EXPECT().CloneRepository(config.RepositoryURL, config.Branch, config.Token, "user", "mail").Return(repository, worktree, "/tmp", nil)

	// The platform check fails → updateSharedCode aborts.
	mockComposer.EXPECT().CheckPlatformReqs(mock.Anything, "/tmp").Return("php 8.1.0 failed", errors.New("unmet platform requirements"))

	// installCode (and a site install) may run concurrently before the cancel propagates.
	mockComposer.EXPECT().Install(mock.Anything, "/tmp").Return(nil).Maybe()
	installer.EXPECT().Install(mock.Anything, "/tmp", "site1").Return(nil).Maybe()

	// Execute
	workflowService := NewWorkflowBaseService(logger, config, drush, vcsProvider, repositoryService, installer, mockComposer, event.NewManager(""))
	err := workflowService.StartUpdate(ctx, nil)

	// Assert: aborts with a clear message and never updates or publishes.
	assert.ErrorContains(t, err, "PHP platform requirements not satisfied")
	mockComposer.AssertNotCalled(t, "Update", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	repository.AssertNotCalled(t, "Push", mock.Anything)
	vcsProvider.AssertNotCalled(t, "CreateMergeRequest", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
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

	vcsProvider.EXPECT().GetUser(mock.Anything).Return("user", "mail")

	// installCode: one CloneRepository + Install
	// updateSharedCode: one CloneRepository + Update (returns empty → AbortError)
	repositoryService.EXPECT().CloneRepository(config.RepositoryURL, config.Branch, config.Token, "user", "mail").Return(repository, worktree, "/tmp", nil).Times(2)
	mockComposer.EXPECT().CheckPlatformReqs(mock.Anything, "/tmp").Return("", nil)

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

	vcsProvider.EXPECT().GetUser(mock.Anything).Return("user", "mail")

	repositoryService.EXPECT().CloneRepository(config.RepositoryURL, config.Branch, config.Token, "user", "mail").Return(repository, worktree, "/tmp", nil).Times(2)
	mockComposer.EXPECT().CheckPlatformReqs(mock.Anything, "/tmp").Return("", nil)
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
	mockComposer.EXPECT().CheckPlatformReqs(mock.Anything, "/tmp").Return("", nil)
	repositoryService.EXPECT().BranchExists(repository, mock.Anything).Return(false, nil)

	vcsProvider.EXPECT().GetUser(mock.Anything).Return("user", "mail")

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

func TestGenerateDescription_UnknownTemplate(t *testing.T) {
	logger := zap.NewNop()
	ws := NewWorkflowBaseService(logger, internal.Config{}, nil, nil, nil, nil, nil, nil)

	_, err := ws.GenerateDescription(nil, "nonexistent.go.tmpl")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to execute template")
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

	vcsProvider.EXPECT().GetUser(mock.Anything).Return("user", "mail")

	// installCode and updateSharedCode each clone the repository.
	repositoryService.EXPECT().CloneRepository(config.RepositoryURL, config.Branch, config.Token, "user", "mail").
		Return(repository, worktree, "/tmp", nil).Times(2)

	mockComposer.EXPECT().Install(mock.Anything, "/tmp").Return(nil)
	mockComposer.EXPECT().CheckPlatformReqs(mock.Anything, "/tmp").Return("", nil)

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
