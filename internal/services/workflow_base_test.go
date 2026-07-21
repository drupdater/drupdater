package services

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/drupdater/drupdater/internal"
	"github.com/drupdater/drupdater/internal/codehosting"
	"github.com/drupdater/drupdater/pkg/composer"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/gookit/event"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
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
		Clone:         true,
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

	repositoryService.EXPECT().CloneRepository(config.RepositoryURL, config.Branch, config.Token, "user", "mail").Return(repository, worktree, "/tmp", nil)
	mockComposer.EXPECT().CheckPlatformReqs(mock.Anything, "/tmp").Return("", nil)
	repositoryService.EXPECT().BranchExists(repository, mock.Anything, mock.Anything).Return(false, nil)

	repository.EXPECT().Push(mock.Anything).Return(nil)

	fixture, err := os.ReadFile("testdata/dependency_update.md")
	require.NoError(t, err, "Failed to read test fixture")

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
	require.NoError(t, err)
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
		Clone:         true,
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

	repositoryService.EXPECT().CloneRepository(config.RepositoryURL, config.Branch, config.Token, "user", "mail").Return(repository, worktree, "/tmp", nil)
	mockComposer.EXPECT().CheckPlatformReqs(mock.Anything, "/tmp").Return("", nil)
	repositoryService.EXPECT().BranchExists(repository, mock.Anything, mock.Anything).Return(false, nil)

	repository.EXPECT().Push(mock.Anything).Return(nil)

	fixture, err := os.ReadFile("testdata/dependency_update.md")
	require.NoError(t, err, "Failed to read test fixture")

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

	require.NoError(t, err)
	require.NoError(t, publishCtxErr, "publishWork must receive a live (non-cancelled) context")
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
		Clone:         true,
		Sites:         []string{"site1"},
		DryRun:        false,
	}

	// updateSharedCode completes fully (site updates only start once it's done).
	worktree := NewMockWorktree(t)
	worktree.EXPECT().Commit(mock.Anything, mock.Anything).Return(plumbing.NewHash(""), nil)
	worktree.EXPECT().AddGlob(mock.Anything).Return(nil)
	worktree.EXPECT().Checkout(mock.Anything).Return(nil)

	repositoryService.EXPECT().CloneRepository(config.RepositoryURL, config.Branch, config.Token, "user", "mail").Return(repository, worktree, "/tmp", nil)
	mockComposer.EXPECT().CheckPlatformReqs(mock.Anything, "/tmp").Return("", nil)
	repositoryService.EXPECT().BranchExists(repository, mock.Anything, mock.Anything).Return(false, nil)

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
	require.ErrorIs(t, err, updateErr)
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
		Clone:         true,
		Sites:         []string{"site1"},
		Timeout:       20 * time.Millisecond,
	}

	worktree := NewMockWorktree(t)
	vcsProvider.EXPECT().GetUser(mock.Anything).Return("user", "mail")
	repositoryService.EXPECT().CloneRepository(config.RepositoryURL, config.Branch, config.Token, "user", "mail").Return(repository, worktree, "/tmp", nil)
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
	require.ErrorIs(t, err, context.DeadlineExceeded)
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
		Clone:         true,
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
	require.ErrorContains(t, err, "PHP platform requirements not satisfied")
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
		Clone:         true,
		Sites:         []string{"site1"},
		DryRun:        false,
	}

	// installCode runs concurrently with updateSharedCode and always completes.
	// installSite may or may not run depending on goroutine scheduling after cancel.
	worktree := NewMockWorktree(t)
	// updateSharedCode checks out a dedicated work branch before doing any work.
	worktree.EXPECT().Checkout(mock.Anything).Return(nil).Maybe()

	vcsProvider.EXPECT().GetUser(mock.Anything).Return("user", "mail")

	// installCode: one CloneRepository + Install
	// updateSharedCode: one CloneRepository + Update (returns empty → AbortError)
	repositoryService.EXPECT().CloneRepository(config.RepositoryURL, config.Branch, config.Token, "user", "mail").Return(repository, worktree, "/tmp", nil)
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
	require.ErrorAs(t, err, &abortErr)
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
		Clone:         true,
		Sites:         []string{"site1"},
		DryRun:        false,
	}

	worktree := NewMockWorktree(t)
	worktree.EXPECT().Commit(mock.Anything, mock.Anything).Return(plumbing.NewHash(""), nil)
	worktree.EXPECT().AddGlob(mock.Anything).Return(nil)
	// updateSharedCode checks out a dedicated work branch before doing any work.
	worktree.EXPECT().Checkout(mock.Anything).Return(nil).Maybe()

	vcsProvider.EXPECT().GetUser(mock.Anything).Return("user", "mail")

	repositoryService.EXPECT().CloneRepository(config.RepositoryURL, config.Branch, config.Token, "user", "mail").Return(repository, worktree, "/tmp", nil)
	mockComposer.EXPECT().CheckPlatformReqs(mock.Anything, "/tmp").Return("", nil)
	repositoryService.EXPECT().BranchExists(repository, mock.Anything, mock.Anything).Return(true, nil)

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
	require.ErrorAs(t, err, &abortErr)
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
		Clone:         true,
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

	repositoryService.EXPECT().CloneRepository(config.RepositoryURL, config.Branch, config.Token, "user", "mail").Return(repository, worktree, "/tmp", nil)
	mockComposer.EXPECT().CheckPlatformReqs(mock.Anything, "/tmp").Return("", nil)
	repositoryService.EXPECT().BranchExists(repository, mock.Anything, mock.Anything).Return(false, nil)

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
	require.NoError(t, err)
	installer.AssertExpectations(t)
	repositoryService.AssertExpectations(t)
	repository.AssertExpectations(t)
	drush.AssertExpectations(t)
	mockComposer.AssertExpectations(t)
	vcsProvider.AssertExpectations(t)
}

func TestPublishWorkDeletesBranchOnMRFailure(t *testing.T) {
	// When CreateMergeRequest fails, publishWork must delete the remote branch it just
	// pushed so the remote is left clean for the next run.
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
		Clone:         true,
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

	repositoryService.EXPECT().CloneRepository(config.RepositoryURL, config.Branch, config.Token, "user", "mail").Return(repository, worktree, "/tmp", nil)
	mockComposer.EXPECT().CheckPlatformReqs(mock.Anything, "/tmp").Return("", nil)
	repositoryService.EXPECT().BranchExists(repository, mock.Anything, mock.Anything).Return(false, nil)

	repository.EXPECT().Push(mock.Anything).Return(nil)

	vcsProvider.EXPECT().GetUser(mock.Anything).Return("user", "mail")

	mrErr := errors.New("API rate limit exceeded")
	vcsProvider.EXPECT().CreateMergeRequest(mock.Anything, mock.Anything, mock.Anything, mock.Anything, config.Branch).Return(codehosting.MergeRequest{}, mrErr)
	vcsProvider.EXPECT().DeleteBranch(mock.Anything, mock.Anything).Return(nil)

	mockComposer.EXPECT().Update(mock.Anything, "/tmp", mock.Anything, mock.Anything, false, false).Return([]composer.PackageChange{
		{Package: "drupal/core", From: "9.0.0", To: "9.1.0"},
	}, nil)
	mockComposer.EXPECT().Install(mock.Anything, "/tmp").Return(nil)
	mockComposer.EXPECT().GetLockHash("/tmp").Return("dummy-hash", nil)

	workflowService := NewWorkflowBaseService(logger, config, drush, vcsProvider, repositoryService, installer, mockComposer, event.NewManager(""))
	err := workflowService.StartUpdate(ctx, nil)

	require.ErrorIs(t, err, mrErr)
	vcsProvider.AssertExpectations(t)
}

func TestPublishWorkLogsWarningWhenDeleteBranchFails(t *testing.T) {
	// If DeleteBranch also fails, publishWork logs a warning but still returns the
	// original MR creation error (best-effort cleanup).
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
		Clone:         true,
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

	repositoryService.EXPECT().CloneRepository(config.RepositoryURL, config.Branch, config.Token, "user", "mail").Return(repository, worktree, "/tmp", nil)
	mockComposer.EXPECT().CheckPlatformReqs(mock.Anything, "/tmp").Return("", nil)
	repositoryService.EXPECT().BranchExists(repository, mock.Anything, mock.Anything).Return(false, nil)

	repository.EXPECT().Push(mock.Anything).Return(nil)

	vcsProvider.EXPECT().GetUser(mock.Anything).Return("user", "mail")

	mrErr := errors.New("API rate limit exceeded")
	deleteErr := errors.New("permission denied")
	vcsProvider.EXPECT().CreateMergeRequest(mock.Anything, mock.Anything, mock.Anything, mock.Anything, config.Branch).Return(codehosting.MergeRequest{}, mrErr)
	vcsProvider.EXPECT().DeleteBranch(mock.Anything, mock.Anything).Return(deleteErr)

	mockComposer.EXPECT().Update(mock.Anything, "/tmp", mock.Anything, mock.Anything, false, false).Return([]composer.PackageChange{
		{Package: "drupal/core", From: "9.0.0", To: "9.1.0"},
	}, nil)
	mockComposer.EXPECT().Install(mock.Anything, "/tmp").Return(nil)
	mockComposer.EXPECT().GetLockHash("/tmp").Return("dummy-hash", nil)

	workflowService := NewWorkflowBaseService(logger, config, drush, vcsProvider, repositoryService, installer, mockComposer, event.NewManager(""))
	err := workflowService.StartUpdate(ctx, nil)

	// The original MR error surfaces; the delete error is only logged.
	require.ErrorIs(t, err, mrErr)
	require.NotErrorIs(t, err, deleteErr)
	vcsProvider.AssertExpectations(t)
}

func TestPublishWorkPushFails(t *testing.T) {
	// When the push fails, publishWork returns the error without attempting MR creation.
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
		Clone:         true,
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

	repositoryService.EXPECT().CloneRepository(config.RepositoryURL, config.Branch, config.Token, "user", "mail").Return(repository, worktree, "/tmp", nil)
	mockComposer.EXPECT().CheckPlatformReqs(mock.Anything, "/tmp").Return("", nil)
	repositoryService.EXPECT().BranchExists(repository, mock.Anything, mock.Anything).Return(false, nil)

	pushErr := errors.New("authentication failed")
	repository.EXPECT().Push(mock.Anything).Return(pushErr)

	vcsProvider.EXPECT().GetUser(mock.Anything).Return("user", "mail")

	mockComposer.EXPECT().Update(mock.Anything, "/tmp", mock.Anything, mock.Anything, false, false).Return([]composer.PackageChange{
		{Package: "drupal/core", From: "9.0.0", To: "9.1.0"},
	}, nil)
	mockComposer.EXPECT().Install(mock.Anything, "/tmp").Return(nil)
	mockComposer.EXPECT().GetLockHash("/tmp").Return("dummy-hash", nil)

	workflowService := NewWorkflowBaseService(logger, config, drush, vcsProvider, repositoryService, installer, mockComposer, event.NewManager(""))
	err := workflowService.StartUpdate(ctx, nil)

	require.ErrorIs(t, err, pushErr)
	vcsProvider.AssertNotCalled(t, "CreateMergeRequest", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestStartUpdateGetLockHashError(t *testing.T) {
	// If GetLockHash fails, updateSharedCode propagates the error.
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
		Clone:         true,
		Sites:         []string{"site1"},
		DryRun:        false,
	}

	worktree := NewMockWorktree(t)
	worktree.EXPECT().Commit(mock.Anything, mock.Anything).Return(plumbing.NewHash(""), nil)
	worktree.EXPECT().AddGlob(mock.Anything).Return(nil)
	// updateSharedCode checks out a dedicated work branch before doing any work.
	worktree.EXPECT().Checkout(mock.Anything).Return(nil).Maybe()

	vcsProvider.EXPECT().GetUser(mock.Anything).Return("user", "mail")
	repositoryService.EXPECT().CloneRepository(config.RepositoryURL, config.Branch, config.Token, "user", "mail").Return(repository, worktree, "/tmp", nil)
	mockComposer.EXPECT().CheckPlatformReqs(mock.Anything, "/tmp").Return("", nil)
	mockComposer.EXPECT().Install(mock.Anything, "/tmp").Return(nil)
	mockComposer.EXPECT().Update(mock.Anything, "/tmp", mock.Anything, mock.Anything, false, false).Return([]composer.PackageChange{
		{Package: "drupal/core", From: "9.0.0", To: "9.1.0"},
	}, nil)

	hashErr := errors.New("composer.lock not found")
	mockComposer.EXPECT().GetLockHash("/tmp").Return("", hashErr)

	installer.EXPECT().Install(mock.Anything, "/tmp", "site1").Return(nil).Maybe()

	workflowService := NewWorkflowBaseService(logger, config, drush, vcsProvider, repositoryService, installer, mockComposer, event.NewManager(""))
	err := workflowService.StartUpdate(ctx, nil)

	require.ErrorIs(t, err, hashErr)
	repository.AssertNotCalled(t, "Push", mock.Anything)
}

func TestStartUpdateBranchExistsError(t *testing.T) {
	// If BranchExists returns an error, updateSharedCode propagates it.
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
		Clone:         true,
		Sites:         []string{"site1"},
		DryRun:        false,
	}

	worktree := NewMockWorktree(t)
	worktree.EXPECT().Commit(mock.Anything, mock.Anything).Return(plumbing.NewHash(""), nil)
	worktree.EXPECT().AddGlob(mock.Anything).Return(nil)
	// updateSharedCode checks out a dedicated work branch before doing any work.
	worktree.EXPECT().Checkout(mock.Anything).Return(nil).Maybe()

	vcsProvider.EXPECT().GetUser(mock.Anything).Return("user", "mail")
	repositoryService.EXPECT().CloneRepository(config.RepositoryURL, config.Branch, config.Token, "user", "mail").Return(repository, worktree, "/tmp", nil)
	mockComposer.EXPECT().CheckPlatformReqs(mock.Anything, "/tmp").Return("", nil)
	mockComposer.EXPECT().Install(mock.Anything, "/tmp").Return(nil)
	mockComposer.EXPECT().Update(mock.Anything, "/tmp", mock.Anything, mock.Anything, false, false).Return([]composer.PackageChange{
		{Package: "drupal/core", From: "9.0.0", To: "9.1.0"},
	}, nil)
	mockComposer.EXPECT().GetLockHash("/tmp").Return("dummy-hash", nil)

	branchErr := errors.New("git remote unreachable")
	repositoryService.EXPECT().BranchExists(repository, mock.Anything, mock.Anything).Return(false, branchErr)

	installer.EXPECT().Install(mock.Anything, "/tmp", "site1").Return(nil).Maybe()

	workflowService := NewWorkflowBaseService(logger, config, drush, vcsProvider, repositoryService, installer, mockComposer, event.NewManager(""))
	err := workflowService.StartUpdate(ctx, nil)

	require.ErrorContains(t, err, "failed to check if branch exists")
	require.ErrorIs(t, err, branchErr)
}

func TestStartUpdateConfigResaveError(t *testing.T) {
	// If ConfigResave fails, updateSite propagates the error.
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
		Clone:         true,
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
	resaveErr := errors.New("drush config:resave failed")
	drush.EXPECT().ConfigResave(mock.Anything, "/tmp", "site1").Return(resaveErr)

	vcsProvider.EXPECT().GetUser(mock.Anything).Return("user", "mail")
	repositoryService.EXPECT().CloneRepository(config.RepositoryURL, config.Branch, config.Token, "user", "mail").Return(repository, worktree, "/tmp", nil)
	mockComposer.EXPECT().CheckPlatformReqs(mock.Anything, "/tmp").Return("", nil)
	repositoryService.EXPECT().BranchExists(repository, mock.Anything, mock.Anything).Return(false, nil)
	mockComposer.EXPECT().Install(mock.Anything, "/tmp").Return(nil)
	mockComposer.EXPECT().Update(mock.Anything, "/tmp", mock.Anything, mock.Anything, false, false).Return([]composer.PackageChange{
		{Package: "drupal/core", From: "9.0.0", To: "9.1.0"},
	}, nil)
	mockComposer.EXPECT().GetLockHash("/tmp").Return("dummy-hash", nil)

	workflowService := NewWorkflowBaseService(logger, config, drush, vcsProvider, repositoryService, installer, mockComposer, event.NewManager(""))
	err := workflowService.StartUpdate(ctx, nil)

	require.ErrorIs(t, err, resaveErr)
	repository.AssertNotCalled(t, "Push", mock.Anything)
}

func TestStartUpdateExportConfigurationError(t *testing.T) {
	// If ExportConfiguration fails, updateSite propagates the error.
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
		Clone:         true,
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
	drush.EXPECT().ConfigResave(mock.Anything, "/tmp", "site1").Return(nil)
	exportErr := errors.New("drush config:export failed")
	drush.EXPECT().ExportConfiguration(mock.Anything, "/tmp", "site1").Return(exportErr)

	vcsProvider.EXPECT().GetUser(mock.Anything).Return("user", "mail")
	repositoryService.EXPECT().CloneRepository(config.RepositoryURL, config.Branch, config.Token, "user", "mail").Return(repository, worktree, "/tmp", nil)
	mockComposer.EXPECT().CheckPlatformReqs(mock.Anything, "/tmp").Return("", nil)
	repositoryService.EXPECT().BranchExists(repository, mock.Anything, mock.Anything).Return(false, nil)
	mockComposer.EXPECT().Install(mock.Anything, "/tmp").Return(nil)
	mockComposer.EXPECT().Update(mock.Anything, "/tmp", mock.Anything, mock.Anything, false, false).Return([]composer.PackageChange{
		{Package: "drupal/core", From: "9.0.0", To: "9.1.0"},
	}, nil)
	mockComposer.EXPECT().GetLockHash("/tmp").Return("dummy-hash", nil)

	workflowService := NewWorkflowBaseService(logger, config, drush, vcsProvider, repositoryService, installer, mockComposer, event.NewManager(""))
	err := workflowService.StartUpdate(ctx, nil)

	require.ErrorIs(t, err, exportErr)
	repository.AssertNotCalled(t, "Push", mock.Anything)
}

func TestGenerateDescription_UnknownTemplate(t *testing.T) {
	logger := zap.NewNop()
	ws := NewWorkflowBaseService(logger, internal.Config{}, nil, nil, nil, nil, nil, nil)

	_, err := ws.GenerateDescription(nil, "nonexistent.go.tmpl")
	require.Error(t, err)
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
		Clone:         true,
		Sites:         []string{"site1"},
		DryRun:        false,
	}

	worktree := NewMockWorktree(t)
	// updateSharedCode checks out a dedicated work branch before firing the first event.
	worktree.EXPECT().Checkout(mock.Anything).Return(nil).Maybe()

	vcsProvider.EXPECT().GetUser(mock.Anything).Return("user", "mail")

	// The run acquires the working copy by cloning once (--clone mode).
	repositoryService.EXPECT().CloneRepository(config.RepositoryURL, config.Branch, config.Token, "user", "mail").
		Return(repository, worktree, "/tmp", nil)

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

	require.ErrorContains(t, err, "failed to fire event")
	require.ErrorContains(t, err, "event bus unavailable")
}

func TestStartUpdateUsesExistingCheckout(t *testing.T) {
	// Default (no --clone): the workflow opens the existing checkout in place instead of
	// cloning, and operates on that single directory throughout.
	logger := zap.NewNop()
	installer := NewMockInstaller(t)
	repositoryService := NewMockRepository(t)
	vcsProvider := NewMockPlatform(t)
	repository := NewMockGitRepository(t)
	mockComposer := NewMockComposer(t)
	drush := NewMockDrush(t)
	ctx := context.Background()

	// t.TempDir() so cleanup() (which touches the parent dir) stays inside the test sandbox.
	checkout := t.TempDir()
	config := internal.Config{
		Branch:     "main",
		Token:      "token",
		WorkingDir: checkout,
		Sites:      []string{"site1"},
	}

	worktree := NewMockWorktree(t)
	worktree.EXPECT().Commit(mock.Anything, mock.Anything).Return(plumbing.NewHash(""), nil)
	worktree.EXPECT().AddGlob(mock.Anything).Return(nil)
	worktree.EXPECT().Checkout(mock.Anything).Return(nil)

	installer.EXPECT().Install(mock.Anything, checkout, "site1").Return(nil)
	installer.EXPECT().ConfigureDatabase(mock.Anything, checkout, "site1").Return(nil)

	drush.EXPECT().UpdateSite(mock.Anything, checkout, "site1").Return(nil)
	drush.EXPECT().ExportConfiguration(mock.Anything, checkout, "site1").Return(nil)
	drush.EXPECT().ConfigResave(mock.Anything, checkout, "site1").Return(nil)

	vcsProvider.EXPECT().GetUser(mock.Anything).Return("user", "mail")
	repositoryService.EXPECT().OpenRepository(checkout, "user", "mail").Return(repository, worktree, checkout, nil)
	mockComposer.EXPECT().CheckPlatformReqs(mock.Anything, checkout).Return("", nil)
	repositoryService.EXPECT().BranchExists(repository, mock.Anything, mock.Anything).Return(false, nil)
	repository.EXPECT().Push(mock.Anything).Return(nil)

	fixture, err := os.ReadFile("testdata/dependency_update.md")
	require.NoError(t, err, "Failed to read test fixture")
	vcsProvider.EXPECT().CreateMergeRequest(mock.Anything, mock.Anything, string(fixture), mock.Anything, config.Branch).Return(codehosting.MergeRequest{}, nil)

	mockComposer.EXPECT().Update(mock.Anything, checkout, mock.Anything, mock.Anything, false, false).Return([]composer.PackageChange{
		{Package: "drupal/core", From: "9.0.0", To: "9.1.0"},
	}, nil)
	mockComposer.EXPECT().Install(mock.Anything, checkout).Return(nil)
	mockComposer.EXPECT().GetLockHash(checkout).Return("dummy-hash", nil)

	workflowService := NewWorkflowBaseService(logger, config, drush, vcsProvider, repositoryService, installer, mockComposer, event.NewManager(""))
	err = workflowService.StartUpdate(ctx, nil)

	require.NoError(t, err)
	repositoryService.AssertExpectations(t)
	repositoryService.AssertNotCalled(t, "CloneRepository", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestStartUpdateWorkBranchCheckoutError(t *testing.T) {
	// If the up-front work-branch checkout fails, updateSharedCode aborts before touching
	// dependencies and nothing is published.
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
		Clone:         true,
		Sites:         []string{"site1"},
	}

	worktree := NewMockWorktree(t)
	checkoutErr := errors.New("cannot create branch")
	worktree.EXPECT().Checkout(mock.Anything).Return(checkoutErr)

	vcsProvider.EXPECT().GetUser(mock.Anything).Return("user", "mail")
	repositoryService.EXPECT().CloneRepository(config.RepositoryURL, config.Branch, config.Token, "user", "mail").Return(repository, worktree, "/tmp", nil)
	mockComposer.EXPECT().CheckPlatformReqs(mock.Anything, "/tmp").Return("", nil)
	mockComposer.EXPECT().Install(mock.Anything, "/tmp").Return(nil)
	installer.EXPECT().Install(mock.Anything, "/tmp", "site1").Return(nil)

	workflowService := NewWorkflowBaseService(logger, config, drush, vcsProvider, repositoryService, installer, mockComposer, event.NewManager(""))
	err := workflowService.StartUpdate(ctx, nil)

	require.ErrorIs(t, err, checkoutErr)
	require.ErrorContains(t, err, "failed to create work branch")
	repository.AssertNotCalled(t, "Push", mock.Anything)
}

func TestCleanup(t *testing.T) {
	logger := zap.NewNop()

	t.Run("clone mode removes only this run's clone, not a sibling", func(t *testing.T) {
		parent, err := os.MkdirTemp(os.TempDir(), "drupdater-clone-test")
		require.NoError(t, err)
		defer os.RemoveAll(parent)

		clone := filepath.Join(parent, "repoAAA")
		sibling := filepath.Join(parent, "repoBBB")
		require.NoError(t, os.MkdirAll(clone, 0o755))
		require.NoError(t, os.MkdirAll(sibling, 0o755))

		ws := &WorkflowBaseService{logger: logger, config: internal.Config{Clone: true}}
		ws.cleanup(clone)

		assert.NoDirExists(t, clone)
		assert.DirExists(t, sibling) // a concurrent run's clone must survive
	})

	t.Run("clone mode refuses to remove the temp dir itself", func(t *testing.T) {
		ws := &WorkflowBaseService{logger: logger, config: internal.Config{Clone: true}}
		ws.cleanup(os.TempDir())
		assert.DirExists(t, os.TempDir())
	})

	t.Run("checkout mode removes the sqlite databases and private dir", func(t *testing.T) {
		base := t.TempDir()
		work := filepath.Join(base, "checkout")
		require.NoError(t, os.MkdirAll(work, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(base, "site1.sqlite"), []byte("x"), 0o600))
		require.NoError(t, os.MkdirAll(filepath.Join(base, "private"), 0o755))

		ws := &WorkflowBaseService{logger: logger, config: internal.Config{Sites: []string{"site1"}}}
		ws.cleanup(work)

		assert.NoFileExists(t, filepath.Join(base, "site1.sqlite"))
		assert.NoDirExists(t, filepath.Join(base, "private"))
	})
}
