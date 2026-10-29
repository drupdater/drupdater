package workflow

import (
	"context"
	"errors"
	"github.com/drupdater/drupdater/pkg/repo"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/drupdater/drupdater/internal"
	"github.com/drupdater/drupdater/internal/codehosting"
	"github.com/drupdater/drupdater/internal/golden"
	"github.com/drupdater/drupdater/pkg/composer"
	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestRun(t *testing.T) {
	logger := zap.NewNop()
	installer := NewMockInstaller(t)
	repositoryService := NewMockRepository(t)
	vcsProvider := NewMockPlatform(t)
	repository := NewMockGitRepository(t)
	expectUpdateBranch(repository)
	mockComposer := NewMockComposer(t)
	expectVersionLookup(mockComposer)
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
	worktree.EXPECT().Add("config/sync").Return(plumbing.ZeroHash, nil).Maybe()
	worktree.EXPECT().Commit(mock.Anything, mock.Anything).Return(plumbing.NewHash(""), nil)
	worktree.EXPECT().AddGlob(mock.Anything).Return(nil)
	worktree.EXPECT().Status().Return(git.Status{}, nil).Maybe()

	installer.EXPECT().Install(anyCtx, testCheckout, "site1").Return(nil)
	installer.EXPECT().ConfigureDatabase(anyCtx, testCheckout, "site1").Return(nil)
	installer.EXPECT().RestoreSettings(anyCtx, testCheckout, "site1").Return(nil)

	drush.EXPECT().UpdateSite(anyCtx, testCheckout, "site1").Return(nil)
	drush.EXPECT().ExportConfiguration(anyCtx, testCheckout, "site1").Return(nil)
	drush.EXPECT().GetConfigSyncDir(anyCtx, testCheckout, "site1", true).Return("config/sync", nil)
	drush.EXPECT().ConfigResave(anyCtx, testCheckout, "site1").Return(nil)

	repositoryService.EXPECT().CloneRepository(config.RepositoryURL, config.Branch, config.Token, "user", "mail").Return(repo.Checkout{Repository: repository, Worktree: worktree, Path: testCheckout, Author: testAuthor}, nil)
	repositoryService.EXPECT().IsShallowClone(testCheckout).Return(false, nil)
	mockComposer.EXPECT().CheckPlatformReqs(anyCtx, testCheckout).Return("", nil)
	repository.EXPECT().Reference(mock.Anything, mock.Anything).Return(nil, plumbing.ErrReferenceNotFound)
	repositoryService.EXPECT().BranchExists(repository, mock.Anything, mock.Anything).Return(false, nil)

	repository.EXPECT().Push(mock.Anything).Return(nil)

	// Captured rather than matched on: as an expectation a changed description fails as an
	// unexpected call, and the diff that would say what changed is never printed. The other
	// tests below still match on the file, and share this one's regeneration.
	var description string

	vcsProvider.EXPECT().GetUser(mock.Anything).Return("user", "mail")
	vcsProvider.EXPECT().CreateMergeRequest(anyCtx, mock.Anything, mock.Anything, mock.Anything, config.Branch).
		RunAndReturn(func(_ context.Context, _ string, got string, _ string, _ string) (codehosting.MergeRequest, error) {
			description = got
			return codehosting.MergeRequest{}, nil
		})

	mockComposer.EXPECT().Update(anyCtx, testCheckout, mock.Anything, mock.Anything, false, false).Return([]composer.PackageChange{
		{
			Package: "drupal/core",
			From:    "9.0.0",
			To:      "9.1.0",
		},
	}, nil)
	mockComposer.EXPECT().Install(anyCtx, testCheckout).Return(nil)
	mockComposer.EXPECT().GetLockHash(testCheckout).Return("dummy-hash", nil)

	workflowService := New(logger, config, drush, vcsProvider, repositoryService, installer, mockComposer)
	err := workflowService.Run(ctx, nil)

	require.NoError(t, err)
	golden.Assert(t, "testdata/dependency_update.md", description)
	installer.AssertExpectations(t)
	repositoryService.AssertExpectations(t)
	repository.AssertExpectations(t)
	drush.AssertExpectations(t)
	mockComposer.AssertExpectations(t)
	vcsProvider.AssertExpectations(t)
}

func TestRunPublishUsesLiveContext(t *testing.T) {
	// Regression: publishWork must run on the outer context. The errgroup's is cancelled as
	// soon as g.Wait() returns, so CreateMergeRequest would see a dead one.
	logger := zap.NewNop()
	installer := NewMockInstaller(t)
	repositoryService := NewMockRepository(t)
	vcsProvider := NewMockPlatform(t)
	repository := NewMockGitRepository(t)
	expectUpdateBranch(repository)
	mockComposer := NewMockComposer(t)
	expectVersionLookup(mockComposer)
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
	worktree.EXPECT().Add("config/sync").Return(plumbing.ZeroHash, nil).Maybe()
	worktree.EXPECT().Commit(mock.Anything, mock.Anything).Return(plumbing.NewHash(""), nil)
	worktree.EXPECT().AddGlob(mock.Anything).Return(nil)
	worktree.EXPECT().Status().Return(git.Status{}, nil).Maybe()

	installer.EXPECT().Install(anyCtx, testCheckout, "site1").Return(nil)
	installer.EXPECT().ConfigureDatabase(anyCtx, testCheckout, "site1").Return(nil)
	installer.EXPECT().RestoreSettings(anyCtx, testCheckout, "site1").Return(nil)

	drush.EXPECT().UpdateSite(anyCtx, testCheckout, "site1").Return(nil)
	drush.EXPECT().ExportConfiguration(anyCtx, testCheckout, "site1").Return(nil)
	drush.EXPECT().GetConfigSyncDir(anyCtx, testCheckout, "site1", true).Return("config/sync", nil)
	drush.EXPECT().ConfigResave(anyCtx, testCheckout, "site1").Return(nil)

	repositoryService.EXPECT().CloneRepository(config.RepositoryURL, config.Branch, config.Token, "user", "mail").Return(repo.Checkout{Repository: repository, Worktree: worktree, Path: testCheckout, Author: testAuthor}, nil)
	repositoryService.EXPECT().IsShallowClone(testCheckout).Return(false, nil)
	mockComposer.EXPECT().CheckPlatformReqs(anyCtx, testCheckout).Return("", nil)
	repository.EXPECT().Reference(mock.Anything, mock.Anything).Return(nil, plumbing.ErrReferenceNotFound)
	repositoryService.EXPECT().BranchExists(repository, mock.Anything, mock.Anything).Return(false, nil)

	repository.EXPECT().Push(mock.Anything).Return(nil)

	fixture, err := os.ReadFile("testdata/dependency_update.md")
	require.NoError(t, err, "Failed to read test fixture")

	vcsProvider.EXPECT().GetUser(mock.Anything).Return("user", "mail")

	// Capture the context handed to publishWork's MR creation and assert it is not cancelled.
	var publishCtxErr error
	vcsProvider.EXPECT().CreateMergeRequest(anyCtx, mock.Anything, string(fixture), mock.Anything, config.Branch).
		Run(func(ctx context.Context, _ string, _ string, _ string, _ string) {
			publishCtxErr = ctx.Err()
		}).Return(codehosting.MergeRequest{}, nil)

	mockComposer.EXPECT().Update(anyCtx, testCheckout, mock.Anything, mock.Anything, false, false).Return([]composer.PackageChange{
		{Package: "drupal/core", From: "9.0.0", To: "9.1.0"},
	}, nil)
	mockComposer.EXPECT().Install(anyCtx, testCheckout).Return(nil)
	mockComposer.EXPECT().GetLockHash(testCheckout).Return("dummy-hash", nil)

	workflowService := New(logger, config, drush, vcsProvider, repositoryService, installer, mockComposer)
	err = workflowService.Run(ctx, nil)

	require.NoError(t, err)
	require.NoError(t, publishCtxErr, "publishWork must receive a live (non-cancelled) context")
	vcsProvider.AssertExpectations(t)
}

func TestRunSiteFailureDoesNotPublish(t *testing.T) {
	// A failing site update must propagate the error and never push a branch or open an MR.
	logger := zap.NewNop()
	installer := NewMockInstaller(t)
	repositoryService := NewMockRepository(t)
	vcsProvider := NewMockPlatform(t)
	repository := NewMockGitRepository(t)
	expectUpdateBranch(repository)
	mockComposer := NewMockComposer(t)
	expectVersionLookup(mockComposer)
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
	worktree.EXPECT().Status().Return(git.Status{}, nil).Maybe()

	repositoryService.EXPECT().CloneRepository(config.RepositoryURL, config.Branch, config.Token, "user", "mail").Return(repo.Checkout{Repository: repository, Worktree: worktree, Path: testCheckout, Author: testAuthor}, nil)
	repositoryService.EXPECT().IsShallowClone(testCheckout).Return(false, nil)
	mockComposer.EXPECT().CheckPlatformReqs(anyCtx, testCheckout).Return("", nil)
	repository.EXPECT().Reference(mock.Anything, mock.Anything).Return(nil, plumbing.ErrReferenceNotFound)
	repositoryService.EXPECT().BranchExists(repository, mock.Anything, mock.Anything).Return(false, nil)

	vcsProvider.EXPECT().GetUser(mock.Anything).Return("user", "mail")

	mockComposer.EXPECT().Install(anyCtx, testCheckout).Return(nil)
	mockComposer.EXPECT().Update(anyCtx, testCheckout, mock.Anything, mock.Anything, false, false).Return([]composer.PackageChange{
		{Package: "drupal/core", From: "9.0.0", To: "9.1.0"},
	}, nil)
	mockComposer.EXPECT().GetLockHash(testCheckout).Return("dummy-hash", nil)

	installer.EXPECT().Install(anyCtx, testCheckout, "site1").Return(nil)
	installer.EXPECT().ConfigureDatabase(anyCtx, testCheckout, "site1").Return(nil)
	installer.EXPECT().RestoreSettings(anyCtx, testCheckout, "site1").Return(nil)

	// The site update fails. ConfigResave/ExportConfiguration are left unexpected on
	// purpose: if the failure didn't short-circuit, those calls would fail the test.
	updateErr := errors.New("drush updatedb crashed")
	drush.EXPECT().UpdateSite(anyCtx, testCheckout, "site1").Return(updateErr)

	workflowService := New(logger, config, drush, vcsProvider, repositoryService, installer, mockComposer)
	err := workflowService.Run(ctx, nil)

	// Assert: the error surfaces and no MR was published.
	require.ErrorIs(t, err, updateErr)
	// The branch only exists once every commit is made, so a failed site update leaves none.
	repository.AssertNotCalled(t, "SetBranch", mock.Anything, mock.Anything)
	repository.AssertNotCalled(t, "RemoveBranch", mock.Anything)
	repository.AssertNotCalled(t, "Push", mock.Anything)
	vcsProvider.AssertNotCalled(t, "CreateMergeRequest", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestRunTimeout(t *testing.T) {
	// A run-level timeout must cancel the workflow context and surface as a
	// deadline error rather than hanging.
	logger := zap.NewNop()
	installer := NewMockInstaller(t)
	repositoryService := NewMockRepository(t)
	vcsProvider := NewMockPlatform(t)
	repository := NewMockGitRepository(t)
	mockComposer := NewMockComposer(t)
	expectVersionLookup(mockComposer)
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
	repositoryService.EXPECT().CloneRepository(config.RepositoryURL, config.Branch, config.Token, "user", "mail").Return(repo.Checkout{Repository: repository, Worktree: worktree, Path: testCheckout, Author: testAuthor}, nil)
	repositoryService.EXPECT().IsShallowClone(testCheckout).Return(false, nil)
	mockComposer.EXPECT().CheckPlatformReqs(anyCtx, testCheckout).Return("", nil)

	// Both phases block on the context, simulating a wedged subprocess; the run
	// timeout is what releases them.
	mockComposer.EXPECT().Install(anyCtx, testCheckout).RunAndReturn(func(ctx context.Context, _ string) error {
		<-ctx.Done()
		return ctx.Err()
	})
	mockComposer.EXPECT().Update(anyCtx, testCheckout, mock.Anything, mock.Anything, false, false).RunAndReturn(func(ctx context.Context, _ string, _ []string, _ []string, _ bool, _ bool) ([]composer.PackageChange, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}).Maybe()

	workflowService := New(logger, config, drush, vcsProvider, repositoryService, installer, mockComposer)
	err := workflowService.Run(ctx, nil)

	// Assert: the deadline propagates and nothing is published.
	require.ErrorIs(t, err, context.DeadlineExceeded)
	repository.AssertNotCalled(t, "Push", mock.Anything)
	vcsProvider.AssertNotCalled(t, "CreateMergeRequest", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestRunPlatformReqsFail(t *testing.T) {
	// If the runtime PHP does not satisfy the project's platform requirements, the run
	// aborts before composer update and nothing is published.
	logger := zap.NewNop()
	installer := NewMockInstaller(t)
	repositoryService := NewMockRepository(t)
	vcsProvider := NewMockPlatform(t)
	repository := NewMockGitRepository(t)
	mockComposer := NewMockComposer(t)
	expectVersionLookup(mockComposer)
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
	repositoryService.EXPECT().CloneRepository(config.RepositoryURL, config.Branch, config.Token, "user", "mail").Return(repo.Checkout{Repository: repository, Worktree: worktree, Path: testCheckout, Author: testAuthor}, nil)

	// The platform check fails → updateSharedCode aborts.
	repositoryService.EXPECT().IsShallowClone(testCheckout).Return(false, nil)
	mockComposer.EXPECT().CheckPlatformReqs(anyCtx, testCheckout).Return("php 8.1.0 failed", errors.New("unmet platform requirements"))

	// installCode (and a site install) may run concurrently before the cancel propagates.
	mockComposer.EXPECT().Install(anyCtx, testCheckout).Return(nil).Maybe()
	installer.EXPECT().Install(anyCtx, testCheckout, "site1").Return(nil).Maybe()

	workflowService := New(logger, config, drush, vcsProvider, repositoryService, installer, mockComposer)
	err := workflowService.Run(ctx, nil)

	// Assert: aborts with a clear message and never updates or publishes.
	require.ErrorContains(t, err, "PHP platform requirements not satisfied")
	mockComposer.AssertNotCalled(t, "Update", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	repository.AssertNotCalled(t, "Push", mock.Anything)
	vcsProvider.AssertNotCalled(t, "CreateMergeRequest", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestRunNoChanges(t *testing.T) {
	logger := zap.NewNop()
	installer := NewMockInstaller(t)
	repositoryService := NewMockRepository(t)
	vcsProvider := NewMockPlatform(t)
	repository := NewMockGitRepository(t)
	expectUpdateBranch(repository)
	mockComposer := NewMockComposer(t)
	expectVersionLookup(mockComposer)
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

	vcsProvider.EXPECT().GetUser(mock.Anything).Return("user", "mail")

	repositoryService.EXPECT().CloneRepository(config.RepositoryURL, config.Branch, config.Token, "user", "mail").Return(repo.Checkout{Repository: repository, Worktree: worktree, Path: testCheckout, Author: testAuthor}, nil)
	repositoryService.EXPECT().IsShallowClone(testCheckout).Return(false, nil)
	mockComposer.EXPECT().CheckPlatformReqs(anyCtx, testCheckout).Return("", nil)

	mockComposer.EXPECT().Install(anyCtx, testCheckout).Return(nil).Once()
	mockComposer.EXPECT().Update(anyCtx, testCheckout, mock.Anything, mock.Anything, false, false).Return([]composer.PackageChange{}, nil)

	workflowService := New(logger, config, drush, vcsProvider, repositoryService, installer, mockComposer)
	err := workflowService.Run(ctx, nil)

	// Assert: should get an AbortError, not a nil or other error
	var abortErr AbortError
	require.ErrorAs(t, err, &abortErr)
	installer.AssertNotCalled(t, "Install", mock.Anything, mock.Anything, mock.Anything)
	repositoryService.AssertExpectations(t)
	repository.AssertExpectations(t)
	drush.AssertExpectations(t)
	mockComposer.AssertExpectations(t)
	vcsProvider.AssertExpectations(t)
}

func TestRunAbortedByAnAddonInstallsNoSite(t *testing.T) {
	// A --security run on a site without advisories aborts in pre-composer-update. It must do so
	// before the baseline install, the most expensive step of a run.
	installer := NewMockInstaller(t)
	repositoryService := NewMockRepository(t)
	vcsProvider := NewMockPlatform(t)
	repository := NewMockGitRepository(t)
	expectUpdateBranch(repository)
	mockComposer := NewMockComposer(t)
	expectVersionLookup(mockComposer)

	config := internal.Config{RepositoryURL: "https://example.com/repo.git", Branch: "main", Token: "token", Clone: true, Sites: []string{"site1"}}

	worktree := NewMockWorktree(t)
	vcsProvider.EXPECT().GetUser(mock.Anything).Return("user", "mail")
	repositoryService.EXPECT().CloneRepository(config.RepositoryURL, config.Branch, config.Token, "user", "mail").Return(repo.Checkout{Repository: repository, Worktree: worktree, Path: testCheckout, Author: testAuthor}, nil)
	repositoryService.EXPECT().IsShallowClone(testCheckout).Return(false, nil)
	mockComposer.EXPECT().CheckPlatformReqs(anyCtx, testCheckout).Return("", nil)
	mockComposer.EXPECT().Install(anyCtx, testCheckout).Return(nil).Once()

	aborts := hookAddon{Hooks{PreComposerUpdate: Propose(Prepare, func(*PreComposerUpdateEvent) (UpdatePlan, error) {
		return UpdatePlan{}, AbortError{Msg: "No security advisories found"}
	})}}

	err := New(zap.NewNop(), config, NewMockDrush(t), vcsProvider, repositoryService, installer, mockComposer).Run(t.Context(), []Addon{aborts})

	_, aborted := errors.AsType[AbortError](err)
	require.True(t, aborted, "got %v", err)
	installer.AssertNotCalled(t, "Install", mock.Anything, mock.Anything, mock.Anything)
	mockComposer.AssertNotCalled(t, "Update", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestRunUpdatesAsTheAddonsPlanned(t *testing.T) {
	installer := NewMockInstaller(t)
	repositoryService := NewMockRepository(t)
	vcsProvider := NewMockPlatform(t)
	repository := NewMockGitRepository(t)
	expectUpdateBranch(repository)
	mockComposer := NewMockComposer(t)
	expectVersionLookup(mockComposer)

	config := internal.Config{RepositoryURL: "https://example.com/repo.git", Branch: "main", Token: "token", Clone: true, Sites: []string{"site1"}}

	worktree := NewMockWorktree(t)
	vcsProvider.EXPECT().GetUser(mock.Anything).Return("user", "mail")
	repositoryService.EXPECT().CloneRepository(config.RepositoryURL, config.Branch, config.Token, "user", "mail").Return(repo.Checkout{Repository: repository, Worktree: worktree, Path: testCheckout, Author: testAuthor}, nil)
	repositoryService.EXPECT().IsShallowClone(testCheckout).Return(false, nil)
	mockComposer.EXPECT().CheckPlatformReqs(anyCtx, testCheckout).Return("", nil)
	mockComposer.EXPECT().Install(anyCtx, testCheckout).Return(nil).Once()
	mockComposer.EXPECT().Update(anyCtx, testCheckout, []string{"drupal/core"}, []string{"drupal/core:10.2.0"}, true, false).
		Return(nil, nil)

	narrows := hookAddon{Hooks{PreComposerUpdate: Propose(Prepare, func(*PreComposerUpdateEvent) (UpdatePlan, error) {
		return UpdatePlan{Only: []string{"drupal/core"}, MinimalChanges: true}, nil
	})}}
	pins := hookAddon{Hooks{PreComposerUpdate: Propose(Change, func(*PreComposerUpdateEvent) (UpdatePlan, error) {
		return UpdatePlan{Keep: []Pin{{Package: "drupal/core", Version: "10.2.0"}}}, nil
	})}}

	err := New(zap.NewNop(), config, NewMockDrush(t), vcsProvider, repositoryService, installer, mockComposer).Run(t.Context(), []Addon{pins, narrows})

	_, aborted := errors.AsType[AbortError](err)
	require.True(t, aborted, "an update that changes nothing aborts; got %v", err)
}

func TestRunInstallsSitesAtTheOldCode(t *testing.T) {
	// vendor/ must still hold the old code when the baseline is installed: the update is resolved
	// into the lock first, and only installed once every site has its baseline database.
	installer := NewMockInstaller(t)
	repositoryService := NewMockRepository(t)
	vcsProvider := NewMockPlatform(t)
	repository := NewMockGitRepository(t)
	expectUpdateBranch(repository)
	mockComposer := NewMockComposer(t)
	expectVersionLookup(mockComposer)
	drush := NewMockDrush(t)

	config := internal.Config{RepositoryURL: "https://example.com/repo.git", Branch: "main", Token: "token", Clone: true, Sites: []string{"site1"}, DryRun: true}

	worktree := NewMockWorktree(t)
	worktree.EXPECT().Add("config/sync").Return(plumbing.ZeroHash, nil).Maybe()
	worktree.EXPECT().AddGlob(mock.Anything).Return(nil)
	worktree.EXPECT().Status().Return(git.Status{}, nil)
	worktree.EXPECT().Commit(mock.Anything, mock.Anything).Return(plumbing.ZeroHash, nil)

	vcsProvider.EXPECT().GetUser(mock.Anything).Return("user", "mail")
	repositoryService.EXPECT().CloneRepository(config.RepositoryURL, config.Branch, config.Token, "user", "mail").Return(repo.Checkout{Repository: repository, Worktree: worktree, Path: testCheckout, Author: testAuthor}, nil)
	repositoryService.EXPECT().IsShallowClone(testCheckout).Return(false, nil)
	repository.EXPECT().Reference(mock.Anything, mock.Anything).Return(nil, plumbing.ErrReferenceNotFound)
	mockComposer.EXPECT().CheckPlatformReqs(anyCtx, testCheckout).Return("", nil)
	mockComposer.EXPECT().GetLockHash(testCheckout).Return("dummy-hash", nil)

	var steps []string
	mockComposer.EXPECT().Install(anyCtx, testCheckout).RunAndReturn(func(context.Context, string) error {
		steps = append(steps, "composer install")
		return nil
	})
	mockComposer.EXPECT().Update(anyCtx, testCheckout, mock.Anything, mock.Anything, false, false).
		RunAndReturn(func(context.Context, string, []string, []string, bool, bool) ([]composer.PackageChange, error) {
			steps = append(steps, "composer update")
			return []composer.PackageChange{{Action: "Upgrade", Package: "drupal/core", From: "9.0.0", To: "9.1.0"}}, nil
		})
	installer.EXPECT().Install(anyCtx, testCheckout, "site1").RunAndReturn(func(context.Context, string, string) error {
		steps = append(steps, "site install")
		return nil
	})
	installer.EXPECT().ConfigureDatabase(anyCtx, testCheckout, "site1").RunAndReturn(func(context.Context, string, string) error {
		steps = append(steps, "site update")
		return nil
	})
	installer.EXPECT().RestoreSettings(anyCtx, testCheckout, "site1").Return(nil)
	drush.EXPECT().UpdateSite(anyCtx, testCheckout, "site1").Return(nil)
	drush.EXPECT().ConfigResave(anyCtx, testCheckout, "site1").Return(nil)
	drush.EXPECT().ExportConfiguration(anyCtx, testCheckout, "site1").Return(nil)
	drush.EXPECT().GetConfigSyncDir(anyCtx, testCheckout, "site1", true).Return("config/sync", nil)

	core, logs := observer.New(zap.InfoLevel)
	err := New(zap.New(core), config, drush, vcsProvider, repositoryService, installer, mockComposer).Run(t.Context(), nil)

	require.NoError(t, err)
	assert.Equal(t, []string{"composer install", "composer update", "site install", "composer install", "site update"}, steps)

	// The log is the only trace of where a run spent its time between phases.
	var messages []string
	for _, entry := range logs.All() {
		messages = append(messages, entry.Message)
	}
	assert.Subset(t, messages, []string{"resolving dependency updates", "dependencies resolved", "installing updated dependencies"})
	resolved := logs.FilterMessage("dependencies resolved").All()
	require.Len(t, resolved, 1)
	assert.EqualValues(t, 1, resolved[0].ContextMap()["total"])
	assert.EqualValues(t, 1, resolved[0].ContextMap()["upgraded"])
}

func TestUpdateSharedCodeFailures(t *testing.T) {
	newRunner := func(t *testing.T, hooks Hooks) (*run, *MockComposer, *MockWorktree) {
		t.Helper()
		mockComposer := NewMockComposer(t)
		worktree := NewMockWorktree(t)
		return &run{Runner: &Runner{logger: zap.NewNop(), composer: mockComposer}, hooks: NewPipeline([]Addon{hookAddon{hooks}})}, mockComposer, worktree
	}

	t.Run("installing the resolved lock", func(t *testing.T) {
		ws, mockComposer, worktree := newRunner(t, Hooks{})
		mockComposer.EXPECT().Install(anyCtx, testCheckout).Return(assert.AnError)

		err := ws.updateSharedCode(t.Context(), worktree, testCheckout)
		require.ErrorIs(t, err, assert.AnError)
		assert.Contains(t, err.Error(), "failed to install updated dependencies")
	})

	t.Run("post-composer-update", func(t *testing.T) {
		ws, mockComposer, worktree := newRunner(t, Hooks{PostComposerUpdate: On(Change, failing[*PostComposerUpdateEvent])})
		mockComposer.EXPECT().Install(anyCtx, testCheckout).Return(nil)

		err := ws.updateSharedCode(t.Context(), worktree, testCheckout)
		require.ErrorIs(t, err, assert.AnError)
		assert.Contains(t, err.Error(), "post-composer-update hook failed")
	})

	t.Run("staging composer files", func(t *testing.T) {
		ws, mockComposer, worktree := newRunner(t, Hooks{})
		mockComposer.EXPECT().Install(anyCtx, testCheckout).Return(nil)
		worktree.EXPECT().AddGlob("composer.*").Return(assert.AnError)

		err := ws.updateSharedCode(t.Context(), worktree, testCheckout)
		require.ErrorIs(t, err, assert.AnError)
		assert.Contains(t, err.Error(), "failed to add composer.* files")
	})

	t.Run("committing", func(t *testing.T) {
		ws, mockComposer, worktree := newRunner(t, Hooks{})
		mockComposer.EXPECT().Install(anyCtx, testCheckout).Return(nil)
		worktree.EXPECT().AddGlob("composer.*").Return(nil)
		worktree.EXPECT().Status().Return(git.Status{}, nil)
		worktree.EXPECT().Commit(mock.Anything, mock.Anything).Return(plumbing.ZeroHash, assert.AnError)

		err := ws.updateSharedCode(t.Context(), worktree, testCheckout)
		require.ErrorIs(t, err, assert.AnError)
		assert.Contains(t, err.Error(), "failed to commit composer.json and composer.lock")
	})

	t.Run("post-code-update", func(t *testing.T) {
		ws, mockComposer, worktree := newRunner(t, Hooks{PostCodeUpdate: On(Change, failing[*PostCodeUpdateEvent])})
		mockComposer.EXPECT().Install(anyCtx, testCheckout).Return(nil)
		worktree.EXPECT().AddGlob("composer.*").Return(nil)
		worktree.EXPECT().Status().Return(git.Status{}, nil)
		worktree.EXPECT().Commit(mock.Anything, mock.Anything).Return(plumbing.ZeroHash, nil)

		err := ws.updateSharedCode(t.Context(), worktree, testCheckout)
		require.ErrorIs(t, err, assert.AnError)
		assert.Contains(t, err.Error(), "post-code-update hook failed")
	})

}

func TestCreateUpdateBranch(t *testing.T) {
	t.Run("points the branch at HEAD and remembers it", func(t *testing.T) {
		ws := &run{Runner: &Runner{logger: zap.NewNop()}}
		repository := NewMockGitRepository(t)
		repository.EXPECT().Head().Return(testHead, nil)
		repository.EXPECT().SetBranch("update-x", testHead.Hash()).Return(nil)

		require.NoError(t, ws.createUpdateBranch(repository, "update-x"))
		assert.Equal(t, "update-x", ws.updateBranch, "so a later failure can remove it")
	})

	t.Run("reading HEAD fails", func(t *testing.T) {
		ws := &run{Runner: &Runner{logger: zap.NewNop()}}
		repository := NewMockGitRepository(t)
		repository.EXPECT().Head().Return(nil, assert.AnError)

		err := ws.createUpdateBranch(repository, "update-x")
		require.ErrorIs(t, err, assert.AnError)
		assert.Contains(t, err.Error(), "failed to read HEAD")
		assert.Empty(t, ws.updateBranch, "no branch was created, so a failure has none to remove")
	})

	t.Run("creating the branch fails", func(t *testing.T) {
		ws := &run{Runner: &Runner{logger: zap.NewNop()}}
		repository := NewMockGitRepository(t)
		repository.EXPECT().Head().Return(testHead, nil)
		repository.EXPECT().SetBranch("update-x", testHead.Hash()).Return(assert.AnError)

		err := ws.createUpdateBranch(repository, "update-x")
		require.ErrorIs(t, err, assert.AnError)
		assert.Contains(t, err.Error(), "failed to create branch update-x")
		assert.Empty(t, ws.updateBranch)
	})
}

// The runner's own commit is signed explicitly, so no identity has to be in git config.
func TestUpdateSharedCodeSignsItsCommit(t *testing.T) {
	mockComposer := NewMockComposer(t)
	worktree := NewMockWorktree(t)
	ws := &run{Runner: &Runner{logger: zap.NewNop(), composer: mockComposer}, author: testAuthor, hooks: NewPipeline(nil)}
	mockComposer.EXPECT().Install(anyCtx, testCheckout).Return(nil)
	worktree.EXPECT().AddGlob("composer.*").Return(nil)
	worktree.EXPECT().Status().Return(git.Status{}, nil)
	worktree.EXPECT().Commit("Update composer.json and composer.lock", mock.MatchedBy(func(opts *git.CommitOptions) bool {
		return opts.Author.Name == testAuthor.Name && opts.Author.Email == testAuthor.Email &&
			opts.Committer.Name == testAuthor.Name && opts.Committer.Email == testAuthor.Email
	})).Return(plumbing.ZeroHash, nil)

	require.NoError(t, ws.updateSharedCode(t.Context(), worktree, testCheckout))
}

// The update branch has to carry every commit, the exported configuration's included: the run
// commits on a detached HEAD, which a branch created any earlier would not follow.
func TestRunCreatesTheUpdateBranchAfterTheLastCommit(t *testing.T) {
	var events []string
	record := func(event string) { events = append(events, event) }

	installer := NewMockInstaller(t)
	repositoryService := NewMockRepository(t)
	repository := NewMockGitRepository(t)
	mockComposer := NewMockComposer(t)
	expectVersionLookup(mockComposer)
	drush := NewMockDrush(t)
	config := internal.Config{RepositoryURL: "https://example.com/repo.git", Branch: "main", Clone: true, Sites: []string{"site1"}, DryRun: true}

	worktree := NewMockWorktree(t)
	worktree.EXPECT().AddGlob(mock.Anything).Return(nil)
	worktree.EXPECT().Add("config/sync").Return(plumbing.ZeroHash, nil)
	// The exported configuration changed, so its commit is not skipped as empty.
	worktree.EXPECT().Status().Return(git.Status{"config/sync/system.site.yml": {Staging: git.Modified}}, nil)
	worktree.EXPECT().Commit(mock.Anything, mock.Anything).RunAndReturn(func(message string, _ *git.CommitOptions) (plumbing.Hash, error) {
		record("commit: " + message)
		return plumbing.ZeroHash, nil
	})
	repository.EXPECT().Reference(mock.Anything, mock.Anything).Return(nil, plumbing.ErrReferenceNotFound)
	repository.EXPECT().Head().Return(testHead, nil)
	repository.EXPECT().SetBranch("update-dummy-hash", testHead.Hash()).RunAndReturn(func(string, plumbing.Hash) error {
		record("branch")
		return nil
	})

	repositoryService.EXPECT().CloneRepository(config.RepositoryURL, config.Branch, "", "", "").
		Return(repo.Checkout{Repository: repository, Worktree: worktree, Path: testCheckout, Author: testAuthor}, nil)
	repositoryService.EXPECT().IsShallowClone(testCheckout).Return(false, nil)
	mockComposer.EXPECT().CheckPlatformReqs(anyCtx, testCheckout).Return("", nil)
	mockComposer.EXPECT().Install(anyCtx, testCheckout).Return(nil)
	mockComposer.EXPECT().Update(anyCtx, testCheckout, mock.Anything, mock.Anything, false, false).
		Return([]composer.PackageChange{{Package: "drupal/core", From: "9.0.0", To: "9.1.0"}}, nil)
	mockComposer.EXPECT().GetLockHash(testCheckout).Return("dummy-hash", nil)
	mockComposer.EXPECT().GetConfig(anyCtx, testCheckout, "extra.drupal-scaffold.locations.web-root").Return("web", nil)
	installer.EXPECT().Install(anyCtx, testCheckout, "site1").Return(nil)
	installer.EXPECT().ConfigureDatabase(anyCtx, testCheckout, "site1").Return(nil)
	installer.EXPECT().RestoreSettings(anyCtx, testCheckout, "site1").Return(nil)
	drush.EXPECT().UpdateSite(anyCtx, testCheckout, "site1").Return(nil)
	drush.EXPECT().ConfigResave(anyCtx, testCheckout, "site1").Return(nil)
	drush.EXPECT().ExportConfiguration(anyCtx, testCheckout, "site1").Return(nil)
	drush.EXPECT().GetConfigSyncDir(anyCtx, testCheckout, "site1", true).Return("config/sync", nil)

	require.NoError(t, New(zap.NewNop(), config, drush, nil, repositoryService, installer, mockComposer).Run(context.Background(), nil))

	assert.Equal(t, []string{
		"commit: Update composer.json and composer.lock",
		"commit: Update configuration site1",
		"branch",
	}, events)
}

func TestRunBranchAlreadyExists(t *testing.T) {
	logger := zap.NewNop()
	installer := NewMockInstaller(t)
	repositoryService := NewMockRepository(t)
	vcsProvider := NewMockPlatform(t)
	repository := NewMockGitRepository(t)
	mockComposer := NewMockComposer(t)
	expectVersionLookup(mockComposer)
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

	vcsProvider.EXPECT().GetUser(mock.Anything).Return("user", "mail")

	repositoryService.EXPECT().CloneRepository(config.RepositoryURL, config.Branch, config.Token, "user", "mail").Return(repo.Checkout{Repository: repository, Worktree: worktree, Path: testCheckout, Author: testAuthor}, nil)
	repositoryService.EXPECT().IsShallowClone(testCheckout).Return(false, nil)
	mockComposer.EXPECT().CheckPlatformReqs(anyCtx, testCheckout).Return("", nil)
	repository.EXPECT().Reference(mock.Anything, mock.Anything).Return(nil, plumbing.ErrReferenceNotFound)
	repositoryService.EXPECT().BranchExists(repository, mock.Anything, mock.Anything).Return(true, nil)

	mockComposer.EXPECT().Install(anyCtx, testCheckout).Return(nil)
	mockComposer.EXPECT().Update(anyCtx, testCheckout, mock.Anything, mock.Anything, false, false).Return([]composer.PackageChange{
		{
			Package: "drupal/core",
			From:    "9.0.0",
			To:      "9.1.0",
		},
	}, nil)
	mockComposer.EXPECT().GetLockHash(testCheckout).Return("dummy-hash", nil)

	workflowService := New(logger, config, drush, vcsProvider, repositoryService, installer, mockComposer)
	err := workflowService.Run(ctx, nil)

	// Assert: should get an AbortError, not a nil or other error
	var abortErr AbortError
	require.ErrorAs(t, err, &abortErr)
	assert.Contains(t, err.Error(), "already exists")
	repositoryService.AssertExpectations(t)
	installer.AssertNotCalled(t, "Install", mock.Anything, mock.Anything, mock.Anything)
}

func TestRunLocalBranchAlreadyExists(t *testing.T) {
	// Regression: a local ref left by a prior run of the same content hash must abort with the
	// clean AbortError, not go-git's raw message from the checkout that follows — and the
	// remote check must not run at all once the local ref is found.
	logger := zap.NewNop()
	installer := NewMockInstaller(t)
	repositoryService := NewMockRepository(t)
	vcsProvider := NewMockPlatform(t)
	repository := NewMockGitRepository(t)
	mockComposer := NewMockComposer(t)
	expectVersionLookup(mockComposer)
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

	vcsProvider.EXPECT().GetUser(mock.Anything).Return("user", "mail")

	repositoryService.EXPECT().CloneRepository(config.RepositoryURL, config.Branch, config.Token, "user", "mail").Return(repo.Checkout{Repository: repository, Worktree: worktree, Path: testCheckout, Author: testAuthor}, nil)
	repositoryService.EXPECT().IsShallowClone(testCheckout).Return(false, nil)
	mockComposer.EXPECT().CheckPlatformReqs(anyCtx, testCheckout).Return("", nil)
	repository.EXPECT().Reference(plumbing.NewBranchReferenceName("update-dummy-hash"), false).
		Return(plumbing.NewHashReference(plumbing.NewBranchReferenceName("update-dummy-hash"), plumbing.NewHash("stale")), nil)

	mockComposer.EXPECT().Install(anyCtx, testCheckout).Return(nil)
	mockComposer.EXPECT().Update(anyCtx, testCheckout, mock.Anything, mock.Anything, false, false).Return([]composer.PackageChange{
		{Package: "drupal/core", From: "9.0.0", To: "9.1.0"},
	}, nil)
	mockComposer.EXPECT().GetLockHash(testCheckout).Return("dummy-hash", nil)

	workflowService := New(logger, config, drush, vcsProvider, repositoryService, installer, mockComposer)
	err := workflowService.Run(ctx, nil)

	var abortErr AbortError
	require.ErrorAs(t, err, &abortErr)
	assert.Contains(t, err.Error(), "already exists locally")
	repositoryService.AssertNotCalled(t, "BranchExists", mock.Anything, mock.Anything, mock.Anything)
	installer.AssertNotCalled(t, "Install", mock.Anything, mock.Anything, mock.Anything)
}

func TestRunLocalBranchLookupError(t *testing.T) {
	// A Reference lookup failure that is not "not found" (e.g. a corrupt local ref) must
	// surface as an error rather than being silently treated as "no local branch".
	logger := zap.NewNop()
	installer := NewMockInstaller(t)
	repositoryService := NewMockRepository(t)
	vcsProvider := NewMockPlatform(t)
	repository := NewMockGitRepository(t)
	mockComposer := NewMockComposer(t)
	expectVersionLookup(mockComposer)
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

	vcsProvider.EXPECT().GetUser(mock.Anything).Return("user", "mail")

	repositoryService.EXPECT().CloneRepository(config.RepositoryURL, config.Branch, config.Token, "user", "mail").Return(repo.Checkout{Repository: repository, Worktree: worktree, Path: testCheckout, Author: testAuthor}, nil)
	repositoryService.EXPECT().IsShallowClone(testCheckout).Return(false, nil)
	mockComposer.EXPECT().CheckPlatformReqs(anyCtx, testCheckout).Return("", nil)
	lookupErr := errors.New("corrupt ref")
	repository.EXPECT().Reference(mock.Anything, mock.Anything).Return(nil, lookupErr)

	mockComposer.EXPECT().Install(anyCtx, testCheckout).Return(nil)
	mockComposer.EXPECT().Update(anyCtx, testCheckout, mock.Anything, mock.Anything, false, false).Return([]composer.PackageChange{
		{Package: "drupal/core", From: "9.0.0", To: "9.1.0"},
	}, nil)
	mockComposer.EXPECT().GetLockHash(testCheckout).Return("dummy-hash", nil)

	workflowService := New(logger, config, drush, vcsProvider, repositoryService, installer, mockComposer)
	err := workflowService.Run(ctx, nil)

	require.ErrorIs(t, err, lookupErr)
	repositoryService.AssertNotCalled(t, "BranchExists", mock.Anything, mock.Anything, mock.Anything)
	installer.AssertNotCalled(t, "Install", mock.Anything, mock.Anything, mock.Anything)
}

func TestRunWithDryRun(t *testing.T) {
	logger := zap.NewNop()
	installer := NewMockInstaller(t)
	repositoryService := NewMockRepository(t)
	vcsProvider := NewMockPlatform(t)
	repository := NewMockGitRepository(t)
	expectUpdateBranch(repository)
	mockComposer := NewMockComposer(t)
	expectVersionLookup(mockComposer)
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

	worktree := NewMockWorktree(t)
	worktree.EXPECT().Add("config/sync").Return(plumbing.ZeroHash, nil).Maybe()
	worktree.EXPECT().Commit(mock.Anything, mock.Anything).Return(plumbing.NewHash(""), nil)
	worktree.EXPECT().AddGlob(mock.Anything).Return(nil)
	worktree.EXPECT().Status().Return(git.Status{}, nil).Maybe()

	installer.EXPECT().Install(anyCtx, testCheckout, "site1").Return(nil)
	installer.EXPECT().ConfigureDatabase(anyCtx, testCheckout, "site1").Return(nil)
	installer.EXPECT().RestoreSettings(anyCtx, testCheckout, "site1").Return(nil)

	drush.EXPECT().UpdateSite(anyCtx, testCheckout, "site1").Return(nil)
	drush.EXPECT().ExportConfiguration(anyCtx, testCheckout, "site1").Return(nil)
	drush.EXPECT().GetConfigSyncDir(anyCtx, testCheckout, "site1", true).Return("config/sync", nil)
	drush.EXPECT().ConfigResave(anyCtx, testCheckout, "site1").Return(nil)

	repositoryService.EXPECT().CloneRepository(config.RepositoryURL, config.Branch, config.Token, "user", "mail").Return(repo.Checkout{Repository: repository, Worktree: worktree, Path: testCheckout, Author: testAuthor}, nil)
	repositoryService.EXPECT().IsShallowClone(testCheckout).Return(false, nil)
	mockComposer.EXPECT().CheckPlatformReqs(anyCtx, testCheckout).Return("", nil)
	repository.EXPECT().Reference(mock.Anything, mock.Anything).Return(nil, plumbing.ErrReferenceNotFound)

	vcsProvider.EXPECT().GetUser(mock.Anything).Return("user", "mail")

	mockComposer.EXPECT().Update(anyCtx, testCheckout, mock.Anything, mock.Anything, false, false).Return([]composer.PackageChange{
		{
			Package: "drupal/core",
			From:    "9.0.0",
			To:      "9.1.0",
		},
	}, nil)
	mockComposer.EXPECT().Install(anyCtx, testCheckout).Return(nil)
	mockComposer.EXPECT().GetLockHash(testCheckout).Return("dummy-hash", nil)

	workflowService := New(logger, config, drush, vcsProvider, repositoryService, installer, mockComposer)
	err := workflowService.Run(ctx, nil)

	require.NoError(t, err)
	installer.AssertExpectations(t)
	repositoryService.AssertExpectations(t)
	repository.AssertExpectations(t)
	drush.AssertExpectations(t)
	mockComposer.AssertExpectations(t)
	vcsProvider.AssertExpectations(t)
	// A --dry-run never pushes, so ensureUpdateBranchAvailable deliberately skips the remote
	// branch check -- reaching it used to make this configuration fail without a token.
	repositoryService.AssertNotCalled(t, "BranchExists", mock.Anything, mock.Anything, mock.Anything)
}

func TestASecondRunLeavesTheFirstRunsBranchAlone(t *testing.T) {
	installer := NewMockInstaller(t)
	repositoryService := NewMockRepository(t)
	vcsProvider := NewMockPlatform(t)
	first := NewMockGitRepository(t)
	expectUpdateBranch(first)
	mockComposer := NewMockComposer(t)
	expectVersionLookup(mockComposer)
	drush := NewMockDrush(t)

	config := internal.Config{RepositoryURL: "https://example.com/repo.git", Branch: "main", Token: "token", Clone: true, Sites: []string{"site1"}, DryRun: true}

	worktree := NewMockWorktree(t)
	worktree.EXPECT().Add("config/sync").Return(plumbing.ZeroHash, nil).Maybe()
	worktree.EXPECT().Commit(mock.Anything, mock.Anything).Return(plumbing.NewHash(""), nil)
	worktree.EXPECT().AddGlob(mock.Anything).Return(nil)
	worktree.EXPECT().Status().Return(git.Status{}, nil).Maybe()
	installer.EXPECT().Install(anyCtx, testCheckout, "site1").Return(nil)
	installer.EXPECT().ConfigureDatabase(anyCtx, testCheckout, "site1").Return(nil)
	installer.EXPECT().RestoreSettings(anyCtx, testCheckout, "site1").Return(nil)
	drush.EXPECT().UpdateSite(anyCtx, testCheckout, "site1").Return(nil)
	drush.EXPECT().ExportConfiguration(anyCtx, testCheckout, "site1").Return(nil)
	drush.EXPECT().GetConfigSyncDir(anyCtx, testCheckout, "site1", true).Return("config/sync", nil)
	drush.EXPECT().ConfigResave(anyCtx, testCheckout, "site1").Return(nil)
	first.EXPECT().Reference(mock.Anything, mock.Anything).Return(nil, plumbing.ErrReferenceNotFound)
	vcsProvider.EXPECT().GetUser(mock.Anything).Return("user", "mail")
	mockComposer.EXPECT().CheckPlatformReqs(anyCtx, testCheckout).Return("", nil)
	mockComposer.EXPECT().Update(anyCtx, testCheckout, mock.Anything, mock.Anything, false, false).
		Return([]composer.PackageChange{{Package: "drupal/core", From: "9.0.0", To: "9.1.0"}}, nil)
	mockComposer.EXPECT().Install(anyCtx, testCheckout).Return(nil)
	mockComposer.EXPECT().GetLockHash(testCheckout).Return("dummy-hash", nil)

	// The second run gets a repository of its own and fails in preflight, after the first run
	// created update-dummy-hash. No RemoveBranch expectation: the mock fails if it is called.
	second := NewMockGitRepository(t)
	repositoryService.EXPECT().CloneRepository(config.RepositoryURL, config.Branch, config.Token, "user", "mail").
		Return(repo.Checkout{Repository: first, Worktree: worktree, Path: testCheckout, Author: testAuthor}, nil).Once()
	repositoryService.EXPECT().CloneRepository(config.RepositoryURL, config.Branch, config.Token, "user", "mail").
		Return(repo.Checkout{Repository: second, Worktree: worktree, Path: testCheckout, Author: testAuthor}, nil).Once()
	repositoryService.EXPECT().IsShallowClone(testCheckout).Return(false, nil).Once()
	repositoryService.EXPECT().IsShallowClone(testCheckout).Return(true, nil).Once()

	runner := New(zap.NewNop(), config, drush, vcsProvider, repositoryService, installer, mockComposer)

	require.NoError(t, runner.Run(t.Context(), nil))
	require.Error(t, runner.Run(t.Context(), nil), "the second run fails in preflight")

	first.AssertCalled(t, "SetBranch", "update-dummy-hash", testHead.Hash())
	first.AssertNotCalled(t, "RemoveBranch", mock.Anything)
}

func TestRunCheckoutDryRunWithoutPlatform(t *testing.T) {
	// Regression for issue #167: a token-free dry run has no VCS platform, and Run must
	// not dereference nil to get the commit identity.
	logger := zap.NewNop()
	installer := NewMockInstaller(t)
	repositoryService := NewMockRepository(t)
	repository := NewMockGitRepository(t)
	expectUpdateBranch(repository)
	mockComposer := NewMockComposer(t)
	expectVersionLookup(mockComposer)
	drush := NewMockDrush(t)
	ctx := context.Background()

	config := internal.Config{
		WorkingDir: "/checkout",
		Branch:     "main",
		Sites:      []string{"site1"},
		DryRun:     true,
	}

	worktree := NewMockWorktree(t)
	worktree.EXPECT().Add("config/sync").Return(plumbing.ZeroHash, nil).Maybe()
	worktree.EXPECT().Commit(mock.Anything, mock.Anything).Return(plumbing.NewHash(""), nil)
	worktree.EXPECT().AddGlob(mock.Anything).Return(nil)
	worktree.EXPECT().Status().Return(git.Status{}, nil).Maybe()

	installer.EXPECT().Install(anyCtx, testCheckout, "site1").Return(nil)
	installer.EXPECT().ConfigureDatabase(anyCtx, testCheckout, "site1").Return(nil)
	installer.EXPECT().RestoreSettings(anyCtx, testCheckout, "site1").Return(nil)

	drush.EXPECT().UpdateSite(anyCtx, testCheckout, "site1").Return(nil)
	drush.EXPECT().ExportConfiguration(anyCtx, testCheckout, "site1").Return(nil)
	drush.EXPECT().GetConfigSyncDir(anyCtx, testCheckout, "site1", true).Return("config/sync", nil)
	drush.EXPECT().ConfigResave(anyCtx, testCheckout, "site1").Return(nil)

	repositoryService.EXPECT().AddWorktree(anyCtx, config.WorkingDir, "", "").Return(repo.Checkout{Repository: repository, Worktree: worktree, Path: testCheckout, Author: testAuthor}, nil)
	repositoryService.EXPECT().RemoveWorktree(anyCtx, config.WorkingDir, testCheckout).Return(nil)
	repositoryService.EXPECT().IsShallowClone(testCheckout).Return(false, nil)
	mockComposer.EXPECT().CheckPlatformReqs(anyCtx, testCheckout).Return("", nil)
	repository.EXPECT().Reference(mock.Anything, mock.Anything).Return(nil, plumbing.ErrReferenceNotFound)
	// Checkout mode (Clone is unset): Run captures HEAD up front so it can restore the
	// checkout if the run fails. This run succeeds, so it is never used to restore anything.
	repository.EXPECT().Head().Return(plumbing.NewHashReference(plumbing.NewBranchReferenceName("main"), plumbing.NewHash("a")), nil)

	mockComposer.EXPECT().Update(anyCtx, testCheckout, mock.Anything, mock.Anything, false, false).Return([]composer.PackageChange{
		{
			Package: "drupal/core",
			From:    "9.0.0",
			To:      "9.1.0",
		},
	}, nil)
	mockComposer.EXPECT().Install(anyCtx, testCheckout).Return(nil)
	mockComposer.EXPECT().GetLockHash(testCheckout).Return("dummy-hash", nil)

	// Execute: platform is nil, exactly as cmd/root.go leaves it for this configuration.
	workflowService := New(logger, config, drush, nil, repositoryService, installer, mockComposer)
	err := workflowService.Run(ctx, nil)

	require.NoError(t, err)
	installer.AssertExpectations(t)
	repositoryService.AssertExpectations(t)
	repository.AssertExpectations(t)
	drush.AssertExpectations(t)
	mockComposer.AssertExpectations(t)
	// A --dry-run never pushes, so the remote check is skipped — reaching out used to fail the
	// one configuration documented as needing no token.
	repositoryService.AssertNotCalled(t, "BranchExists", mock.Anything, mock.Anything, mock.Anything)
}

func TestPublishWorkDeletesBranchOnMRFailure(t *testing.T) {
	// When CreateMergeRequest fails, publishWork must delete the remote branch it just
	// pushed so the remote is left clean for the next run.
	logger := zap.NewNop()
	installer := NewMockInstaller(t)
	repositoryService := NewMockRepository(t)
	vcsProvider := NewMockPlatform(t)
	repository := NewMockGitRepository(t)
	expectUpdateBranch(repository)
	mockComposer := NewMockComposer(t)
	expectVersionLookup(mockComposer)
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
	worktree.EXPECT().Add("config/sync").Return(plumbing.ZeroHash, nil).Maybe()
	worktree.EXPECT().Commit(mock.Anything, mock.Anything).Return(plumbing.NewHash(""), nil)
	worktree.EXPECT().AddGlob(mock.Anything).Return(nil)
	worktree.EXPECT().Status().Return(git.Status{}, nil).Maybe()

	installer.EXPECT().Install(anyCtx, testCheckout, "site1").Return(nil)
	installer.EXPECT().ConfigureDatabase(anyCtx, testCheckout, "site1").Return(nil)
	installer.EXPECT().RestoreSettings(anyCtx, testCheckout, "site1").Return(nil)

	drush.EXPECT().UpdateSite(anyCtx, testCheckout, "site1").Return(nil)
	drush.EXPECT().ExportConfiguration(anyCtx, testCheckout, "site1").Return(nil)
	drush.EXPECT().GetConfigSyncDir(anyCtx, testCheckout, "site1", true).Return("config/sync", nil)
	drush.EXPECT().ConfigResave(anyCtx, testCheckout, "site1").Return(nil)

	repositoryService.EXPECT().CloneRepository(config.RepositoryURL, config.Branch, config.Token, "user", "mail").Return(repo.Checkout{Repository: repository, Worktree: worktree, Path: testCheckout, Author: testAuthor}, nil)
	repositoryService.EXPECT().IsShallowClone(testCheckout).Return(false, nil)
	mockComposer.EXPECT().CheckPlatformReqs(anyCtx, testCheckout).Return("", nil)
	repository.EXPECT().Reference(mock.Anything, mock.Anything).Return(nil, plumbing.ErrReferenceNotFound)
	repositoryService.EXPECT().BranchExists(repository, mock.Anything, mock.Anything).Return(false, nil)

	repository.EXPECT().Push(mock.Anything).Return(nil)

	vcsProvider.EXPECT().GetUser(mock.Anything).Return("user", "mail")

	mrErr := errors.New("API rate limit exceeded")
	vcsProvider.EXPECT().CreateMergeRequest(anyCtx, mock.Anything, mock.Anything, mock.Anything, config.Branch).Return(codehosting.MergeRequest{}, mrErr)
	vcsProvider.EXPECT().DeleteBranch(anyCtx, mock.Anything).Return(nil)

	mockComposer.EXPECT().Update(anyCtx, testCheckout, mock.Anything, mock.Anything, false, false).Return([]composer.PackageChange{
		{Package: "drupal/core", From: "9.0.0", To: "9.1.0"},
	}, nil)
	mockComposer.EXPECT().Install(anyCtx, testCheckout).Return(nil)
	mockComposer.EXPECT().GetLockHash(testCheckout).Return("dummy-hash", nil)

	workflowService := New(logger, config, drush, vcsProvider, repositoryService, installer, mockComposer)
	err := workflowService.Run(ctx, nil)

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
	expectUpdateBranch(repository)
	mockComposer := NewMockComposer(t)
	expectVersionLookup(mockComposer)
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
	worktree.EXPECT().Add("config/sync").Return(plumbing.ZeroHash, nil).Maybe()
	worktree.EXPECT().Commit(mock.Anything, mock.Anything).Return(plumbing.NewHash(""), nil)
	worktree.EXPECT().AddGlob(mock.Anything).Return(nil)
	worktree.EXPECT().Status().Return(git.Status{}, nil).Maybe()

	installer.EXPECT().Install(anyCtx, testCheckout, "site1").Return(nil)
	installer.EXPECT().ConfigureDatabase(anyCtx, testCheckout, "site1").Return(nil)
	installer.EXPECT().RestoreSettings(anyCtx, testCheckout, "site1").Return(nil)

	drush.EXPECT().UpdateSite(anyCtx, testCheckout, "site1").Return(nil)
	drush.EXPECT().ExportConfiguration(anyCtx, testCheckout, "site1").Return(nil)
	drush.EXPECT().GetConfigSyncDir(anyCtx, testCheckout, "site1", true).Return("config/sync", nil)
	drush.EXPECT().ConfigResave(anyCtx, testCheckout, "site1").Return(nil)

	repositoryService.EXPECT().CloneRepository(config.RepositoryURL, config.Branch, config.Token, "user", "mail").Return(repo.Checkout{Repository: repository, Worktree: worktree, Path: testCheckout, Author: testAuthor}, nil)
	repositoryService.EXPECT().IsShallowClone(testCheckout).Return(false, nil)
	mockComposer.EXPECT().CheckPlatformReqs(anyCtx, testCheckout).Return("", nil)
	repository.EXPECT().Reference(mock.Anything, mock.Anything).Return(nil, plumbing.ErrReferenceNotFound)
	repositoryService.EXPECT().BranchExists(repository, mock.Anything, mock.Anything).Return(false, nil)

	repository.EXPECT().Push(mock.Anything).Return(nil)

	vcsProvider.EXPECT().GetUser(mock.Anything).Return("user", "mail")

	mrErr := errors.New("API rate limit exceeded")
	deleteErr := errors.New("permission denied")
	vcsProvider.EXPECT().CreateMergeRequest(anyCtx, mock.Anything, mock.Anything, mock.Anything, config.Branch).Return(codehosting.MergeRequest{}, mrErr)
	vcsProvider.EXPECT().DeleteBranch(anyCtx, mock.Anything).Return(deleteErr)

	mockComposer.EXPECT().Update(anyCtx, testCheckout, mock.Anything, mock.Anything, false, false).Return([]composer.PackageChange{
		{Package: "drupal/core", From: "9.0.0", To: "9.1.0"},
	}, nil)
	mockComposer.EXPECT().Install(anyCtx, testCheckout).Return(nil)
	mockComposer.EXPECT().GetLockHash(testCheckout).Return("dummy-hash", nil)

	workflowService := New(logger, config, drush, vcsProvider, repositoryService, installer, mockComposer)
	err := workflowService.Run(ctx, nil)

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
	expectUpdateBranch(repository)
	mockComposer := NewMockComposer(t)
	expectVersionLookup(mockComposer)
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
	worktree.EXPECT().Add("config/sync").Return(plumbing.ZeroHash, nil).Maybe()
	worktree.EXPECT().Commit(mock.Anything, mock.Anything).Return(plumbing.NewHash(""), nil)
	worktree.EXPECT().AddGlob(mock.Anything).Return(nil)
	worktree.EXPECT().Status().Return(git.Status{}, nil).Maybe()

	installer.EXPECT().Install(anyCtx, testCheckout, "site1").Return(nil)
	installer.EXPECT().ConfigureDatabase(anyCtx, testCheckout, "site1").Return(nil)
	installer.EXPECT().RestoreSettings(anyCtx, testCheckout, "site1").Return(nil)

	drush.EXPECT().UpdateSite(anyCtx, testCheckout, "site1").Return(nil)
	drush.EXPECT().ExportConfiguration(anyCtx, testCheckout, "site1").Return(nil)
	drush.EXPECT().GetConfigSyncDir(anyCtx, testCheckout, "site1", true).Return("config/sync", nil)
	drush.EXPECT().ConfigResave(anyCtx, testCheckout, "site1").Return(nil)

	repositoryService.EXPECT().CloneRepository(config.RepositoryURL, config.Branch, config.Token, "user", "mail").Return(repo.Checkout{Repository: repository, Worktree: worktree, Path: testCheckout, Author: testAuthor}, nil)
	repositoryService.EXPECT().IsShallowClone(testCheckout).Return(false, nil)
	mockComposer.EXPECT().CheckPlatformReqs(anyCtx, testCheckout).Return("", nil)
	repository.EXPECT().Reference(mock.Anything, mock.Anything).Return(nil, plumbing.ErrReferenceNotFound)
	repositoryService.EXPECT().BranchExists(repository, mock.Anything, mock.Anything).Return(false, nil)

	pushErr := errors.New("authentication failed")
	repository.EXPECT().Push(mock.Anything).Return(pushErr)

	vcsProvider.EXPECT().GetUser(mock.Anything).Return("user", "mail")

	mockComposer.EXPECT().Update(anyCtx, testCheckout, mock.Anything, mock.Anything, false, false).Return([]composer.PackageChange{
		{Package: "drupal/core", From: "9.0.0", To: "9.1.0"},
	}, nil)
	mockComposer.EXPECT().Install(anyCtx, testCheckout).Return(nil)
	mockComposer.EXPECT().GetLockHash(testCheckout).Return("dummy-hash", nil)

	workflowService := New(logger, config, drush, vcsProvider, repositoryService, installer, mockComposer)
	err := workflowService.Run(ctx, nil)

	require.ErrorIs(t, err, pushErr)
	vcsProvider.AssertNotCalled(t, "CreateMergeRequest", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestRunGetLockHashError(t *testing.T) {
	// If GetLockHash fails, resolveUpdate propagates the error.
	logger := zap.NewNop()
	installer := NewMockInstaller(t)
	repositoryService := NewMockRepository(t)
	vcsProvider := NewMockPlatform(t)
	repository := NewMockGitRepository(t)
	mockComposer := NewMockComposer(t)
	expectVersionLookup(mockComposer)
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

	vcsProvider.EXPECT().GetUser(mock.Anything).Return("user", "mail")
	repositoryService.EXPECT().CloneRepository(config.RepositoryURL, config.Branch, config.Token, "user", "mail").Return(repo.Checkout{Repository: repository, Worktree: worktree, Path: testCheckout, Author: testAuthor}, nil)
	repositoryService.EXPECT().IsShallowClone(testCheckout).Return(false, nil)
	mockComposer.EXPECT().CheckPlatformReqs(anyCtx, testCheckout).Return("", nil)
	mockComposer.EXPECT().Install(anyCtx, testCheckout).Return(nil)
	mockComposer.EXPECT().Update(anyCtx, testCheckout, mock.Anything, mock.Anything, false, false).Return([]composer.PackageChange{
		{Package: "drupal/core", From: "9.0.0", To: "9.1.0"},
	}, nil)

	hashErr := errors.New("composer.lock not found")
	mockComposer.EXPECT().GetLockHash(testCheckout).Return("", hashErr)

	workflowService := New(logger, config, drush, vcsProvider, repositoryService, installer, mockComposer)
	err := workflowService.Run(ctx, nil)

	require.ErrorIs(t, err, hashErr)
	repository.AssertNotCalled(t, "Push", mock.Anything)
	installer.AssertNotCalled(t, "Install", mock.Anything, mock.Anything, mock.Anything)
}

func TestRunBranchExistsError(t *testing.T) {
	// If BranchExists returns an error, resolveUpdate propagates it.
	logger := zap.NewNop()
	installer := NewMockInstaller(t)
	repositoryService := NewMockRepository(t)
	vcsProvider := NewMockPlatform(t)
	repository := NewMockGitRepository(t)
	mockComposer := NewMockComposer(t)
	expectVersionLookup(mockComposer)
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

	vcsProvider.EXPECT().GetUser(mock.Anything).Return("user", "mail")
	repositoryService.EXPECT().CloneRepository(config.RepositoryURL, config.Branch, config.Token, "user", "mail").Return(repo.Checkout{Repository: repository, Worktree: worktree, Path: testCheckout, Author: testAuthor}, nil)
	repositoryService.EXPECT().IsShallowClone(testCheckout).Return(false, nil)
	mockComposer.EXPECT().CheckPlatformReqs(anyCtx, testCheckout).Return("", nil)
	mockComposer.EXPECT().Install(anyCtx, testCheckout).Return(nil)
	mockComposer.EXPECT().Update(anyCtx, testCheckout, mock.Anything, mock.Anything, false, false).Return([]composer.PackageChange{
		{Package: "drupal/core", From: "9.0.0", To: "9.1.0"},
	}, nil)
	mockComposer.EXPECT().GetLockHash(testCheckout).Return("dummy-hash", nil)

	branchErr := errors.New("git remote unreachable")
	repository.EXPECT().Reference(mock.Anything, mock.Anything).Return(nil, plumbing.ErrReferenceNotFound)
	repositoryService.EXPECT().BranchExists(repository, mock.Anything, mock.Anything).Return(false, branchErr)

	workflowService := New(logger, config, drush, vcsProvider, repositoryService, installer, mockComposer)
	err := workflowService.Run(ctx, nil)

	require.ErrorContains(t, err, "failed to check if branch exists")
	require.ErrorIs(t, err, branchErr)
	installer.AssertNotCalled(t, "Install", mock.Anything, mock.Anything, mock.Anything)
}

func TestRunConfigResaveError(t *testing.T) {
	// If ConfigResave fails, updateSite propagates the error.
	logger := zap.NewNop()
	installer := NewMockInstaller(t)
	repositoryService := NewMockRepository(t)
	vcsProvider := NewMockPlatform(t)
	repository := NewMockGitRepository(t)
	expectUpdateBranch(repository)
	mockComposer := NewMockComposer(t)
	expectVersionLookup(mockComposer)
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
	worktree.EXPECT().Status().Return(git.Status{}, nil).Maybe()

	installer.EXPECT().Install(anyCtx, testCheckout, "site1").Return(nil)
	installer.EXPECT().ConfigureDatabase(anyCtx, testCheckout, "site1").Return(nil)
	installer.EXPECT().RestoreSettings(anyCtx, testCheckout, "site1").Return(nil)

	drush.EXPECT().UpdateSite(anyCtx, testCheckout, "site1").Return(nil)
	resaveErr := errors.New("drush config:resave failed")
	drush.EXPECT().ConfigResave(anyCtx, testCheckout, "site1").Return(resaveErr)

	vcsProvider.EXPECT().GetUser(mock.Anything).Return("user", "mail")
	repositoryService.EXPECT().CloneRepository(config.RepositoryURL, config.Branch, config.Token, "user", "mail").Return(repo.Checkout{Repository: repository, Worktree: worktree, Path: testCheckout, Author: testAuthor}, nil)
	repositoryService.EXPECT().IsShallowClone(testCheckout).Return(false, nil)
	mockComposer.EXPECT().CheckPlatformReqs(anyCtx, testCheckout).Return("", nil)
	repository.EXPECT().Reference(mock.Anything, mock.Anything).Return(nil, plumbing.ErrReferenceNotFound)
	repositoryService.EXPECT().BranchExists(repository, mock.Anything, mock.Anything).Return(false, nil)
	mockComposer.EXPECT().Install(anyCtx, testCheckout).Return(nil)
	mockComposer.EXPECT().Update(anyCtx, testCheckout, mock.Anything, mock.Anything, false, false).Return([]composer.PackageChange{
		{Package: "drupal/core", From: "9.0.0", To: "9.1.0"},
	}, nil)
	mockComposer.EXPECT().GetLockHash(testCheckout).Return("dummy-hash", nil)

	workflowService := New(logger, config, drush, vcsProvider, repositoryService, installer, mockComposer)
	err := workflowService.Run(ctx, nil)

	require.ErrorIs(t, err, resaveErr)
	repository.AssertNotCalled(t, "Push", mock.Anything)
}

func TestRunExportConfigurationError(t *testing.T) {
	// If ExportConfiguration fails, updateSite propagates the error.
	logger := zap.NewNop()
	installer := NewMockInstaller(t)
	repositoryService := NewMockRepository(t)
	vcsProvider := NewMockPlatform(t)
	repository := NewMockGitRepository(t)
	expectUpdateBranch(repository)
	mockComposer := NewMockComposer(t)
	expectVersionLookup(mockComposer)
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
	worktree.EXPECT().Add("config/sync").Return(plumbing.ZeroHash, nil).Maybe()
	worktree.EXPECT().Commit(mock.Anything, mock.Anything).Return(plumbing.NewHash(""), nil)
	worktree.EXPECT().AddGlob(mock.Anything).Return(nil)
	worktree.EXPECT().Status().Return(git.Status{}, nil).Maybe()

	installer.EXPECT().Install(anyCtx, testCheckout, "site1").Return(nil)
	installer.EXPECT().ConfigureDatabase(anyCtx, testCheckout, "site1").Return(nil)
	installer.EXPECT().RestoreSettings(anyCtx, testCheckout, "site1").Return(nil)

	drush.EXPECT().UpdateSite(anyCtx, testCheckout, "site1").Return(nil)
	drush.EXPECT().ConfigResave(anyCtx, testCheckout, "site1").Return(nil)
	exportErr := errors.New("drush config:export failed")
	drush.EXPECT().ExportConfiguration(anyCtx, testCheckout, "site1").Return(exportErr)

	vcsProvider.EXPECT().GetUser(mock.Anything).Return("user", "mail")
	repositoryService.EXPECT().CloneRepository(config.RepositoryURL, config.Branch, config.Token, "user", "mail").Return(repo.Checkout{Repository: repository, Worktree: worktree, Path: testCheckout, Author: testAuthor}, nil)
	repositoryService.EXPECT().IsShallowClone(testCheckout).Return(false, nil)
	mockComposer.EXPECT().CheckPlatformReqs(anyCtx, testCheckout).Return("", nil)
	repository.EXPECT().Reference(mock.Anything, mock.Anything).Return(nil, plumbing.ErrReferenceNotFound)
	repositoryService.EXPECT().BranchExists(repository, mock.Anything, mock.Anything).Return(false, nil)
	mockComposer.EXPECT().Install(anyCtx, testCheckout).Return(nil)
	mockComposer.EXPECT().Update(anyCtx, testCheckout, mock.Anything, mock.Anything, false, false).Return([]composer.PackageChange{
		{Package: "drupal/core", From: "9.0.0", To: "9.1.0"},
	}, nil)
	mockComposer.EXPECT().GetLockHash(testCheckout).Return("dummy-hash", nil)

	workflowService := New(logger, config, drush, vcsProvider, repositoryService, installer, mockComposer)
	err := workflowService.Run(ctx, nil)

	require.ErrorIs(t, err, exportErr)
	repository.AssertNotCalled(t, "Push", mock.Anything)
}

func TestRunWithAutoMerge(t *testing.T) {
	// When AutoMerge.Normal is true and Security is false, EnableAutoMerge must be called.
	logger := zap.NewNop()
	installer := NewMockInstaller(t)
	repositoryService := NewMockRepository(t)
	vcsProvider := NewMockPlatform(t)
	repository := NewMockGitRepository(t)
	expectUpdateBranch(repository)
	mockComposer := NewMockComposer(t)
	expectVersionLookup(mockComposer)
	drush := NewMockDrush(t)
	ctx := context.Background()

	config := internal.Config{
		RepositoryURL: "https://example.com/repo.git",
		Branch:        "main",
		Token:         "token",
		Clone:         true,
		Sites:         []string{"site1"},
		DryRun:        false,
		RunTypes:      internal.RunTypesConfig{Normal: internal.RunTypeConfig{AutoMerge: true}},
	}

	worktree := NewMockWorktree(t)
	worktree.EXPECT().Add("config/sync").Return(plumbing.ZeroHash, nil).Maybe()
	worktree.EXPECT().Commit(mock.Anything, mock.Anything).Return(plumbing.NewHash(""), nil)
	worktree.EXPECT().AddGlob(mock.Anything).Return(nil)
	worktree.EXPECT().Status().Return(git.Status{}, nil).Maybe()

	installer.EXPECT().Install(anyCtx, testCheckout, "site1").Return(nil)
	installer.EXPECT().ConfigureDatabase(anyCtx, testCheckout, "site1").Return(nil)
	installer.EXPECT().RestoreSettings(anyCtx, testCheckout, "site1").Return(nil)

	drush.EXPECT().UpdateSite(anyCtx, testCheckout, "site1").Return(nil)
	drush.EXPECT().ExportConfiguration(anyCtx, testCheckout, "site1").Return(nil)
	drush.EXPECT().GetConfigSyncDir(anyCtx, testCheckout, "site1", true).Return("config/sync", nil)
	drush.EXPECT().ConfigResave(anyCtx, testCheckout, "site1").Return(nil)

	repositoryService.EXPECT().CloneRepository(config.RepositoryURL, config.Branch, config.Token, "user", "mail").Return(repo.Checkout{Repository: repository, Worktree: worktree, Path: testCheckout, Author: testAuthor}, nil)
	repositoryService.EXPECT().IsShallowClone(testCheckout).Return(false, nil)
	mockComposer.EXPECT().CheckPlatformReqs(anyCtx, testCheckout).Return("", nil)
	repository.EXPECT().Reference(mock.Anything, mock.Anything).Return(nil, plumbing.ErrReferenceNotFound)
	repositoryService.EXPECT().BranchExists(repository, mock.Anything, mock.Anything).Return(false, nil)
	repository.EXPECT().Push(mock.Anything).Return(nil)

	fixture, err := os.ReadFile("testdata/dependency_update.md")
	require.NoError(t, err)

	createdMR := codehosting.MergeRequest{ID: 42, URL: "http://example.com/mr/42"}
	vcsProvider.EXPECT().GetUser(mock.Anything).Return("user", "mail")
	vcsProvider.EXPECT().CreateMergeRequest(anyCtx, mock.Anything, string(fixture), mock.Anything, config.Branch).Return(createdMR, nil)
	vcsProvider.EXPECT().EnableAutoMerge(anyCtx, createdMR).Return(nil)

	mockComposer.EXPECT().Update(anyCtx, testCheckout, mock.Anything, mock.Anything, false, false).Return([]composer.PackageChange{
		{Package: "drupal/core", From: "9.0.0", To: "9.1.0"},
	}, nil)
	mockComposer.EXPECT().Install(anyCtx, testCheckout).Return(nil)
	mockComposer.EXPECT().GetLockHash(testCheckout).Return("dummy-hash", nil)

	workflowService := New(logger, config, drush, vcsProvider, repositoryService, installer, mockComposer)
	err = workflowService.Run(ctx, nil)

	require.NoError(t, err)
	vcsProvider.AssertExpectations(t)
}

func TestRunAutoMergeError(t *testing.T) {
	// Auto-merge is a convenience on top of work that already succeeded: the branch is pushed
	// and the MR exists. A failure here must be logged as a warning, not fail the run.
	core, logs := observer.New(zap.WarnLevel)
	logger := zap.New(core)
	installer := NewMockInstaller(t)
	repositoryService := NewMockRepository(t)
	vcsProvider := NewMockPlatform(t)
	repository := NewMockGitRepository(t)
	expectUpdateBranch(repository)
	mockComposer := NewMockComposer(t)
	expectVersionLookup(mockComposer)
	drush := NewMockDrush(t)
	ctx := context.Background()

	config := internal.Config{
		RepositoryURL: "https://example.com/repo.git",
		Branch:        "main",
		Token:         "token",
		Clone:         true,
		Sites:         []string{"site1"},
		DryRun:        false,
		RunTypes:      internal.RunTypesConfig{Normal: internal.RunTypeConfig{AutoMerge: true}},
	}

	worktree := NewMockWorktree(t)
	worktree.EXPECT().Add("config/sync").Return(plumbing.ZeroHash, nil).Maybe()
	worktree.EXPECT().Commit(mock.Anything, mock.Anything).Return(plumbing.NewHash(""), nil)
	worktree.EXPECT().AddGlob(mock.Anything).Return(nil)
	worktree.EXPECT().Status().Return(git.Status{}, nil).Maybe()

	installer.EXPECT().Install(anyCtx, testCheckout, "site1").Return(nil)
	installer.EXPECT().ConfigureDatabase(anyCtx, testCheckout, "site1").Return(nil)
	installer.EXPECT().RestoreSettings(anyCtx, testCheckout, "site1").Return(nil)

	drush.EXPECT().UpdateSite(anyCtx, testCheckout, "site1").Return(nil)
	drush.EXPECT().ExportConfiguration(anyCtx, testCheckout, "site1").Return(nil)
	drush.EXPECT().GetConfigSyncDir(anyCtx, testCheckout, "site1", true).Return("config/sync", nil)
	drush.EXPECT().ConfigResave(anyCtx, testCheckout, "site1").Return(nil)

	repositoryService.EXPECT().CloneRepository(config.RepositoryURL, config.Branch, config.Token, "user", "mail").Return(repo.Checkout{Repository: repository, Worktree: worktree, Path: testCheckout, Author: testAuthor}, nil)
	repositoryService.EXPECT().IsShallowClone(testCheckout).Return(false, nil)
	mockComposer.EXPECT().CheckPlatformReqs(anyCtx, testCheckout).Return("", nil)
	repository.EXPECT().Reference(mock.Anything, mock.Anything).Return(nil, plumbing.ErrReferenceNotFound)
	repositoryService.EXPECT().BranchExists(repository, mock.Anything, mock.Anything).Return(false, nil)
	repository.EXPECT().Push(mock.Anything).Return(nil)

	createdMR := codehosting.MergeRequest{ID: 42, URL: "http://example.com/mr/42"}
	vcsProvider.EXPECT().GetUser(mock.Anything).Return("user", "mail")
	vcsProvider.EXPECT().CreateMergeRequest(anyCtx, mock.Anything, mock.Anything, mock.Anything, config.Branch).Return(createdMR, nil)
	autoMergeErr := errors.New("auto-merge not allowed")
	vcsProvider.EXPECT().EnableAutoMerge(anyCtx, createdMR).Return(autoMergeErr)

	mockComposer.EXPECT().Update(anyCtx, testCheckout, mock.Anything, mock.Anything, false, false).Return([]composer.PackageChange{
		{Package: "drupal/core", From: "9.0.0", To: "9.1.0"},
	}, nil)
	mockComposer.EXPECT().Install(anyCtx, testCheckout).Return(nil)
	mockComposer.EXPECT().GetLockHash(testCheckout).Return("dummy-hash", nil)

	workflowService := New(logger, config, drush, vcsProvider, repositoryService, installer, mockComposer)
	err := workflowService.Run(ctx, nil)

	require.NoError(t, err, "a failed auto-merge must not fail the run — the MR was created")

	warnings := logs.FilterMessage("failed to enable auto merge").All()
	require.Len(t, warnings, 1, "the failure must still be reported to the operator")
	assert.Contains(t, warnings[0].ContextMap()["error"], autoMergeErr.Error())
}

func TestRunAutoMergeSkippedWhenDisabled(t *testing.T) {
	// With AutoMerge zero value (all false), EnableAutoMerge must NOT be called.
	logger := zap.NewNop()
	installer := NewMockInstaller(t)
	repositoryService := NewMockRepository(t)
	vcsProvider := NewMockPlatform(t)
	repository := NewMockGitRepository(t)
	expectUpdateBranch(repository)
	mockComposer := NewMockComposer(t)
	expectVersionLookup(mockComposer)
	drush := NewMockDrush(t)
	ctx := context.Background()

	config := internal.Config{
		RepositoryURL: "https://example.com/repo.git",
		Branch:        "main",
		Token:         "token",
		Clone:         true,
		Sites:         []string{"site1"},
		DryRun:        false,
		RunTypes:      internal.RunTypesConfig{},
	}

	worktree := NewMockWorktree(t)
	worktree.EXPECT().Add("config/sync").Return(plumbing.ZeroHash, nil).Maybe()
	worktree.EXPECT().Commit(mock.Anything, mock.Anything).Return(plumbing.NewHash(""), nil)
	worktree.EXPECT().AddGlob(mock.Anything).Return(nil)
	worktree.EXPECT().Status().Return(git.Status{}, nil).Maybe()

	installer.EXPECT().Install(anyCtx, testCheckout, "site1").Return(nil)
	installer.EXPECT().ConfigureDatabase(anyCtx, testCheckout, "site1").Return(nil)
	installer.EXPECT().RestoreSettings(anyCtx, testCheckout, "site1").Return(nil)

	drush.EXPECT().UpdateSite(anyCtx, testCheckout, "site1").Return(nil)
	drush.EXPECT().ExportConfiguration(anyCtx, testCheckout, "site1").Return(nil)
	drush.EXPECT().GetConfigSyncDir(anyCtx, testCheckout, "site1", true).Return("config/sync", nil)
	drush.EXPECT().ConfigResave(anyCtx, testCheckout, "site1").Return(nil)

	repositoryService.EXPECT().CloneRepository(config.RepositoryURL, config.Branch, config.Token, "user", "mail").Return(repo.Checkout{Repository: repository, Worktree: worktree, Path: testCheckout, Author: testAuthor}, nil)
	repositoryService.EXPECT().IsShallowClone(testCheckout).Return(false, nil)
	mockComposer.EXPECT().CheckPlatformReqs(anyCtx, testCheckout).Return("", nil)
	repository.EXPECT().Reference(mock.Anything, mock.Anything).Return(nil, plumbing.ErrReferenceNotFound)
	repositoryService.EXPECT().BranchExists(repository, mock.Anything, mock.Anything).Return(false, nil)
	repository.EXPECT().Push(mock.Anything).Return(nil)

	fixture, err := os.ReadFile("testdata/dependency_update.md")
	require.NoError(t, err)

	vcsProvider.EXPECT().GetUser(mock.Anything).Return("user", "mail")
	vcsProvider.EXPECT().CreateMergeRequest(anyCtx, mock.Anything, string(fixture), mock.Anything, config.Branch).Return(codehosting.MergeRequest{}, nil)

	mockComposer.EXPECT().Update(anyCtx, testCheckout, mock.Anything, mock.Anything, false, false).Return([]composer.PackageChange{
		{Package: "drupal/core", From: "9.0.0", To: "9.1.0"},
	}, nil)
	mockComposer.EXPECT().Install(anyCtx, testCheckout).Return(nil)
	mockComposer.EXPECT().GetLockHash(testCheckout).Return("dummy-hash", nil)

	workflowService := New(logger, config, drush, vcsProvider, repositoryService, installer, mockComposer)
	err = workflowService.Run(ctx, nil)

	require.NoError(t, err)
	vcsProvider.AssertNotCalled(t, "EnableAutoMerge", mock.Anything, mock.Anything)
}

func TestRunFireEventError(t *testing.T) {
	// Verify that a FireEvent error is propagated out of Run.
	logger := zap.NewNop()
	installer := NewMockInstaller(t)
	repositoryService := NewMockRepository(t)
	vcsProvider := NewMockPlatform(t)
	repository := NewMockGitRepository(t)
	mockComposer := NewMockComposer(t)
	expectVersionLookup(mockComposer)
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
	worktree.EXPECT().Add("config/sync").Return(plumbing.ZeroHash, nil).Maybe()
	// updateSharedCode checks out a dedicated work branch before firing the first event.

	vcsProvider.EXPECT().GetUser(mock.Anything).Return("user", "mail")

	// The run acquires the working copy by cloning once (--clone mode).
	repositoryService.EXPECT().CloneRepository(config.RepositoryURL, config.Branch, config.Token, "user", "mail").
		Return(repo.Checkout{Repository: repository, Worktree: worktree, Path: testCheckout, Author: testAuthor}, nil)

	mockComposer.EXPECT().Install(anyCtx, testCheckout).Return(nil)
	repositoryService.EXPECT().IsShallowClone(testCheckout).Return(false, nil)
	mockComposer.EXPECT().CheckPlatformReqs(anyCtx, testCheckout).Return("", nil)

	fails := hookAddon{Hooks{PreComposerUpdate: Propose(Change, func(*PreComposerUpdateEvent) (UpdatePlan, error) {
		return UpdatePlan{}, errors.New("audit unavailable")
	})}}

	// installSite may or may not run before the context is cancelled.
	installer.EXPECT().Install(anyCtx, testCheckout, "site1").Return(nil).Maybe()
	installer.EXPECT().ConfigureDatabase(anyCtx, testCheckout, "site1").Return(nil).Maybe()
	installer.EXPECT().RestoreSettings(anyCtx, testCheckout, "site1").Return(nil).Maybe()
	drush.EXPECT().UpdateSite(anyCtx, testCheckout, "site1").Return(nil).Maybe()
	drush.EXPECT().ExportConfiguration(anyCtx, testCheckout, "site1").Return(nil).Maybe()
	drush.EXPECT().ConfigResave(anyCtx, testCheckout, "site1").Return(nil).Maybe()

	workflowService := New(logger, config, drush, vcsProvider, repositoryService, installer, mockComposer)
	err := workflowService.Run(ctx, []Addon{fails})

	require.ErrorContains(t, err, "pre-composer-update hook failed")
	require.ErrorContains(t, err, "audit unavailable")
}

// checkoutModeRun is a checkout-mode run that gets all the way to publishing, working in a
// worktree at worktreePath of the checkout at source.
func checkoutModeRun(t *testing.T, source, worktreePath string) (*Runner, *MockRepository, *MockGitRepository) {
	t.Helper()
	installer := NewMockInstaller(t)
	repositoryService := NewMockRepository(t)
	vcsProvider := NewMockPlatform(t)
	repository := NewMockGitRepository(t)
	expectUpdateBranch(repository)
	mockComposer := NewMockComposer(t)
	expectVersionLookup(mockComposer)
	drush := NewMockDrush(t)

	config := internal.Config{Branch: "main", Token: "token", WorkingDir: source, Sites: []string{"site1"}}

	worktree := NewMockWorktree(t)
	worktree.EXPECT().Commit(mock.Anything, mock.Anything).Return(plumbing.NewHash(""), nil)
	worktree.EXPECT().AddGlob(mock.Anything).Return(nil)
	worktree.EXPECT().Add("config/sync").Return(plumbing.ZeroHash, nil)
	worktree.EXPECT().Status().Return(git.Status{}, nil).Maybe()

	installer.EXPECT().Install(anyCtx, worktreePath, "site1").Return(nil)
	installer.EXPECT().ConfigureDatabase(anyCtx, worktreePath, "site1").Return(nil)
	installer.EXPECT().RestoreSettings(anyCtx, worktreePath, "site1").Return(nil)
	drush.EXPECT().UpdateSite(anyCtx, worktreePath, "site1").Return(nil)
	drush.EXPECT().ExportConfiguration(anyCtx, worktreePath, "site1").Return(nil)
	drush.EXPECT().GetConfigSyncDir(anyCtx, worktreePath, "site1", true).Return("config/sync", nil)
	drush.EXPECT().ConfigResave(anyCtx, worktreePath, "site1").Return(nil)

	vcsProvider.EXPECT().GetUser(mock.Anything).Return("user", "mail")
	repositoryService.EXPECT().AddWorktree(anyCtx, source, "user", "mail").
		Return(repo.Checkout{Repository: repository, Worktree: worktree, Path: worktreePath, Author: testAuthor}, nil)
	repositoryService.EXPECT().IsShallowClone(worktreePath).Return(false, nil)
	mockComposer.EXPECT().CheckPlatformReqs(anyCtx, worktreePath).Return("", nil)
	repository.EXPECT().Reference(mock.Anything, mock.Anything).Return(nil, plumbing.ErrReferenceNotFound)
	repositoryService.EXPECT().BranchExists(repository, mock.Anything, mock.Anything).Return(false, nil)
	repository.EXPECT().Push(mock.Anything).Return(nil)
	vcsProvider.EXPECT().CreateMergeRequest(anyCtx, mock.Anything, mock.Anything, "update-dummy-hash", config.Branch).Return(codehosting.MergeRequest{}, nil)

	mockComposer.EXPECT().Update(anyCtx, worktreePath, mock.Anything, mock.Anything, false, false).Return([]composer.PackageChange{
		{Package: "drupal/core", From: "9.0.0", To: "9.1.0"},
	}, nil)
	mockComposer.EXPECT().Install(anyCtx, worktreePath).Return(nil)
	mockComposer.EXPECT().GetLockHash(worktreePath).Return("dummy-hash", nil)

	return New(zap.NewNop(), config, drush, vcsProvider, repositoryService, installer, mockComposer), repositoryService, repository
}

// Without --clone the run works in a worktree of the checkout, never in the checkout itself, and
// removes the worktree when done. The update branch is all it leaves behind.
func TestRunWorksInAWorktreeOfTheCheckout(t *testing.T) {
	source, worktreePath := t.TempDir(), t.TempDir()
	runner, repositoryService, repository := checkoutModeRun(t, source, worktreePath)
	repositoryService.EXPECT().RemoveWorktree(anyCtx, source, worktreePath).Return(nil)

	require.NoError(t, runner.Run(context.Background(), nil))

	repository.AssertCalled(t, "SetBranch", "update-dummy-hash", testHead.Hash())
	repository.AssertNotCalled(t, "RemoveBranch", mock.Anything)
	repositoryService.AssertNotCalled(t, "CloneRepository", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// A worktree that will not go away is a leftover temporary directory, not a failed update.
func TestRunOnlyWarnsWhenItsWorktreeCannotBeRemoved(t *testing.T) {
	source, worktreePath := t.TempDir(), t.TempDir()
	runner, repositoryService, _ := checkoutModeRun(t, source, worktreePath)
	repositoryService.EXPECT().RemoveWorktree(anyCtx, source, worktreePath).Return(assert.AnError)
	core, logs := observer.New(zap.WarnLevel)
	runner.logger = zap.New(core)

	require.NoError(t, runner.Run(context.Background(), nil))

	warnings := logs.FilterMessage("failed to remove the run's worktree").All()
	require.Len(t, warnings, 1)
	assert.Equal(t, worktreePath, warnings[0].ContextMap()["path"])
}

// Cancelling the run must not stop it from removing its worktree.
func TestRunRemovesItsWorktreeEvenWhenCancelled(t *testing.T) {
	source := t.TempDir()
	installer := NewMockInstaller(t)
	repositoryService := NewMockRepository(t)
	mockComposer := NewMockComposer(t)
	expectVersionLookup(mockComposer)
	ctx, cancel := context.WithCancel(context.Background())

	repositoryService.EXPECT().AddWorktree(anyCtx, source, "", "").
		Return(repo.Checkout{Repository: NewMockGitRepository(t), Worktree: NewMockWorktree(t), Path: testCheckout, Author: testAuthor}, nil)
	repositoryService.EXPECT().IsShallowClone(testCheckout).RunAndReturn(func(string) (bool, error) {
		cancel()
		return false, context.Canceled
	})
	repositoryService.EXPECT().RemoveWorktree(mock.MatchedBy(func(ctx context.Context) bool { return ctx.Err() == nil }), source, testCheckout).Return(nil)

	config := internal.Config{Branch: "main", WorkingDir: source, Sites: []string{"site1"}, DryRun: true}
	err := New(zap.NewNop(), config, NewMockDrush(t), nil, repositoryService, installer, mockComposer).Run(ctx, nil)

	require.Error(t, err)
}

func TestRemoveUpdateBranch(t *testing.T) {
	t.Run("nothing to remove before a branch was created", func(t *testing.T) {
		ws := &run{Runner: &Runner{logger: zap.NewNop()}}

		ws.removeUpdateBranch(NewMockGitRepository(t))
	})

	t.Run("a failure to remove only warns", func(t *testing.T) {
		core, logs := observer.New(zap.WarnLevel)
		ws := &run{Runner: &Runner{logger: zap.New(core)}, updateBranch: "update-x"}
		repository := NewMockGitRepository(t)
		repository.EXPECT().RemoveBranch("update-x").Return(assert.AnError)

		ws.removeUpdateBranch(repository)

		warnings := logs.FilterMessage("failed to remove the failed run's update branch").All()
		require.Len(t, warnings, 1)
		assert.Equal(t, "update-x", warnings[0].ContextMap()["branch"])
	})
}

func TestForEachSite(t *testing.T) {
	logger := zap.NewNop()
	sites := []string{"site1", "site2", "site3", "site4"}

	t.Run("bounds concurrency to config.Concurrency", func(t *testing.T) {
		ws := &Runner{logger: logger, config: internal.Config{Sites: sites, Concurrency: 1}}

		var current, peak atomic.Int32
		err := ws.forEachSite(context.Background(), func(_ context.Context, _ int, _ string) error {
			n := current.Add(1)
			defer current.Add(-1)
			for {
				m := peak.Load()
				if n <= m || peak.CompareAndSwap(m, n) {
					break
				}
			}
			time.Sleep(time.Millisecond)
			return nil
		})

		require.NoError(t, err)
		assert.EqualValues(t, 1, peak.Load())
	})

	t.Run("falls back to GOMAXPROCS(0) when Concurrency is unset", func(t *testing.T) {
		ws := &Runner{logger: logger, config: internal.Config{Sites: sites}}

		var calls atomic.Int32
		err := ws.forEachSite(context.Background(), func(_ context.Context, _ int, _ string) error {
			calls.Add(1)
			return nil
		})

		require.NoError(t, err)
		assert.EqualValues(t, len(sites), calls.Load())
	})

	t.Run("propagates the first error and cancels the rest", func(t *testing.T) {
		ws := &Runner{logger: logger, config: internal.Config{Sites: sites, Concurrency: 2}}
		boom := errors.New("boom")

		err := ws.forEachSite(context.Background(), func(_ context.Context, _ int, site string) error {
			if site == "site1" {
				return boom
			}
			return nil
		})

		require.ErrorIs(t, err, boom)
	})
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

		ws := &Runner{logger: logger, config: internal.Config{Clone: true}}
		ws.cleanup(t.Context(), clone)

		assert.NoDirExists(t, clone)
		assert.DirExists(t, sibling) // a concurrent run's clone must survive
	})

	t.Run("clone mode refuses to remove the temp dir itself", func(t *testing.T) {
		// This asks cleanup() to delete the temp dir and relies on its guard to refuse, so
		// os.TempDir() is pointed at a sandbox: a broken guard must not wipe the real one.
		t.Setenv("TMPDIR", t.TempDir())

		ws := &Runner{logger: logger, config: internal.Config{Clone: true}}
		ws.cleanup(t.Context(), os.TempDir())
		assert.DirExists(t, os.TempDir())
	})

	t.Run("checkout mode removes the run's worktree and touches no directory itself", func(t *testing.T) {
		base := t.TempDir()
		work := filepath.Join(base, "checkout")
		require.NoError(t, os.MkdirAll(work, 0o755))
		repositoryService := NewMockRepository(t)
		repositoryService.EXPECT().RemoveWorktree(anyCtx, "/source", work).Return(nil)

		ws := &Runner{logger: logger, repository: repositoryService, config: internal.Config{WorkingDir: "/source", Sites: []string{"site1"}}}
		ws.cleanup(t.Context(), work)

		assert.DirExists(t, work, "removing is the repository service's job, through git")
	})
}

func TestUpdateSiteRestoresSettings(t *testing.T) {
	newRunner := func(t *testing.T, addons ...Addon) (*run, *MockInstaller, *MockDrush) {
		t.Helper()
		installer := NewMockInstaller(t)
		drush := NewMockDrush(t)
		return &run{Runner: &Runner{logger: zap.NewNop(), installer: installer, drush: drush}, hooks: NewPipeline(addons)}, installer, drush
	}

	t.Run("after a failed pre-site-update hook, which stops the update", func(t *testing.T) {
		ws, installer, _ := newRunner(t, hookAddon{Hooks{PreSiteUpdate: On(Inspect, failing[*PreSiteUpdateEvent])}})
		installer.EXPECT().ConfigureDatabase(anyCtx, "/project", "site1").Return(nil)
		installer.EXPECT().RestoreSettings(anyCtx, "/project", "site1").Return(nil).Once()

		err := ws.updateSite(t.Context(), "/project", "site1", yourTurn(), make(chan struct{}))
		require.ErrorIs(t, err, assert.AnError)
		assert.Contains(t, err.Error(), "pre-site-update hook failed")
	})

	t.Run("after a failed post-site-update hook, which stops the export", func(t *testing.T) {
		ws, installer, drush := newRunner(t, hookAddon{Hooks{PostSiteUpdate: On(Change, failing[*PostSiteUpdateEvent])}})
		installer.EXPECT().ConfigureDatabase(anyCtx, "/project", "site1").Return(nil)
		drush.EXPECT().UpdateSite(anyCtx, "/project", "site1").Return(nil)
		drush.EXPECT().ConfigResave(anyCtx, "/project", "site1").Return(nil)
		installer.EXPECT().RestoreSettings(anyCtx, "/project", "site1").Return(nil).Once()

		err := ws.updateSite(t.Context(), "/project", "site1", yourTurn(), make(chan struct{}))
		require.ErrorIs(t, err, assert.AnError)
		assert.Contains(t, err.Error(), "post-site-update hook failed")
	})

	t.Run("after a failed update hook", func(t *testing.T) {
		ws, installer, drush := newRunner(t)
		installer.EXPECT().ConfigureDatabase(anyCtx, "/project", "site1").Return(nil)
		drush.EXPECT().UpdateSite(anyCtx, "/project", "site1").Return(assert.AnError)
		installer.EXPECT().RestoreSettings(anyCtx, "/project", "site1").Return(nil).Once()

		err := ws.updateSite(t.Context(), "/project", "site1", yourTurn(), make(chan struct{}))
		require.ErrorIs(t, err, assert.AnError)
		assert.NotContains(t, err.Error(), "restore")
	})

	t.Run("with a context another site cancelled", func(t *testing.T) {
		ws, installer, drush := newRunner(t)
		ctx, cancel := context.WithCancel(t.Context())
		installer.EXPECT().ConfigureDatabase(anyCtx, "/project", "site1").Return(nil)
		drush.EXPECT().UpdateSite(anyCtx, "/project", "site1").RunAndReturn(func(context.Context, string, string) error {
			cancel()
			return context.Canceled
		})
		installer.EXPECT().RestoreSettings(mock.MatchedBy(func(ctx context.Context) bool { return ctx.Err() == nil }), "/project", "site1").Return(nil).Once()

		require.ErrorIs(t, ws.updateSite(ctx, "/project", "site1", yourTurn(), make(chan struct{})), context.Canceled)
	})

	t.Run("a failed restore fails the site", func(t *testing.T) {
		ws, installer, drush := newRunner(t)
		installer.EXPECT().ConfigureDatabase(anyCtx, "/project", "site1").Return(nil)
		drush.EXPECT().UpdateSite(anyCtx, "/project", "site1").Return(nil)
		drush.EXPECT().ConfigResave(anyCtx, "/project", "site1").Return(nil)
		drush.EXPECT().ExportConfiguration(anyCtx, "/project", "site1").Return(nil)
		drush.EXPECT().GetConfigSyncDir(anyCtx, "/project", "site1", true).Return("config/sync", nil)
		committer := NewMockCommitter(t)
		committer.EXPECT().Commit("Update configuration site1", []string{"config/sync"}).Return(true, nil)
		ws.committer = committer
		installer.EXPECT().RestoreSettings(anyCtx, "/project", "site1").Return(assert.AnError)

		err := ws.updateSite(t.Context(), "/project", "site1", yourTurn(), make(chan struct{}))
		require.ErrorIs(t, err, assert.AnError)
		assert.Contains(t, err.Error(), "failed to restore settings")
	})

	t.Run("not when configuring failed", func(t *testing.T) {
		ws, installer, _ := newRunner(t)
		installer.EXPECT().ConfigureDatabase(anyCtx, "/project", "site1").Return(assert.AnError)

		require.ErrorIs(t, ws.updateSite(t.Context(), "/project", "site1", yourTurn(), make(chan struct{})), assert.AnError)
	})
}

// Drupdater commits the exported configuration itself, as it does an addon's changes: Drush's own
// --commit would run git, its hooks and its identity, in the user's repository.
func TestCommitSiteChangesCommitsTheExportedConfiguration(t *testing.T) {
	newRunner := func(t *testing.T) (*run, *MockDrush, *MockCommitter) {
		t.Helper()
		drush := NewMockDrush(t)
		committer := NewMockCommitter(t)
		drush.EXPECT().ExportConfiguration(anyCtx, "/project", "site1").Return(nil)
		return &run{Runner: &Runner{logger: zap.NewNop(), drush: drush}, committer: committer, hooks: NewPipeline(nil)}, drush, committer
	}

	t.Run("the site's sync directory, under its own message", func(t *testing.T) {
		ws, drush, committer := newRunner(t)
		drush.EXPECT().GetConfigSyncDir(anyCtx, "/project", "site1", true).Return("config/site1", nil)
		committer.EXPECT().Commit("Update configuration site1", []string{"config/site1"}).Return(true, nil)

		require.NoError(t, ws.commitSiteChanges(t.Context(), "/project", "site1"))
	})

	t.Run("an unknown sync directory", func(t *testing.T) {
		ws, drush, _ := newRunner(t)
		drush.EXPECT().GetConfigSyncDir(anyCtx, "/project", "site1", true).Return("", assert.AnError)

		err := ws.commitSiteChanges(t.Context(), "/project", "site1")
		require.ErrorIs(t, err, assert.AnError)
		require.ErrorContains(t, err, "failed to locate the exported configuration")
	})

	t.Run("no sync directory at all", func(t *testing.T) {
		ws, drush, _ := newRunner(t)
		drush.EXPECT().GetConfigSyncDir(anyCtx, "/project", "site1", true).Return("", nil)

		err := ws.commitSiteChanges(t.Context(), "/project", "site1")
		require.ErrorContains(t, err, "site site1 has no config sync directory")
	})

	t.Run("a refused commit", func(t *testing.T) {
		ws, drush, committer := newRunner(t)
		drush.EXPECT().GetConfigSyncDir(anyCtx, "/project", "site1", true).Return("config/site1", nil)
		committer.EXPECT().Commit("Update configuration site1", []string{"config/site1"}).Return(false, assert.AnError)

		err := ws.commitSiteChanges(t.Context(), "/project", "site1")
		require.ErrorIs(t, err, assert.AnError)
		require.ErrorContains(t, err, "failed to commit the exported configuration")
	})
}

func TestEnsureUpdateBranchAvailable(t *testing.T) {
	const branch = "update-abc123"

	newCheckout := func(t *testing.T) *MockGitRepository {
		t.Helper()
		checkout := NewMockGitRepository(t)
		checkout.EXPECT().Reference(plumbing.NewBranchReferenceName(branch), false).Return(nil, plumbing.ErrReferenceNotFound)
		return checkout
	}

	t.Run("a dry run never reaches the remote", func(t *testing.T) {
		// A --dry-run never pushes, so the remote is never reached. BranchExists is left
		// unstubbed, so the mock fails the test if it is called.
		repository := NewMockRepository(t)
		ws := &Runner{
			logger:     zap.NewNop(),
			repository: repository,
			config:     internal.Config{DryRun: true},
		}

		require.NoError(t, ws.ensureUpdateBranchAvailable(newCheckout(t), branch))
	})

	t.Run("a real run asks the remote and aborts when the branch is taken", func(t *testing.T) {
		checkout := newCheckout(t)
		repository := NewMockRepository(t)
		repository.EXPECT().BranchExists(checkout, branch, "tok").Return(true, nil)
		ws := &Runner{
			logger:     zap.NewNop(),
			repository: repository,
			config:     internal.Config{DryRun: false, Token: "tok"},
		}

		err := ws.ensureUpdateBranchAvailable(checkout, branch)
		var abort AbortError
		require.ErrorAs(t, err, &abort)
	})

	t.Run("a real run proceeds when the remote does not have the branch", func(t *testing.T) {
		checkout := newCheckout(t)
		repository := NewMockRepository(t)
		repository.EXPECT().BranchExists(checkout, branch, "tok").Return(false, nil)
		ws := &Runner{
			logger:     zap.NewNop(),
			repository: repository,
			config:     internal.Config{DryRun: false, Token: "tok"},
		}

		require.NoError(t, ws.ensureUpdateBranchAvailable(checkout, branch))
	})

	t.Run("a remote failure is surfaced", func(t *testing.T) {
		checkout := newCheckout(t)
		repository := NewMockRepository(t)
		repository.EXPECT().BranchExists(checkout, branch, "").Return(false, assert.AnError)
		ws := &Runner{
			logger:     zap.NewNop(),
			repository: repository,
			config:     internal.Config{DryRun: false},
		}

		err := ws.ensureUpdateBranchAvailable(checkout, branch)
		require.ErrorContains(t, err, "failed to check if branch exists")
	})
}

// anyCtx matches any non-nil context.Context: the errgroup derives a child, so the exact value
// cannot be pinned, but non-nil still catches a call site that stops propagating it.
// Outside the temp dir, so a mutant of cleanup's guard has nothing real to delete.
const testCheckout = "/drupdater-test/checkout"

var anyCtx = mock.MatchedBy(func(ctx context.Context) bool { return ctx != nil })

// expectVersionLookup declares the version call every run makes. What the versions end up as is
// workflow_report_test.go's subject, not these runs'.
func expectVersionLookup(composerMock *MockComposer) {
	composerMock.EXPECT().Version(anyCtx).Return(composer.Versions{Composer: "2.10.2", PHP: "8.3.14"}, nil)
}

// workBranchCheckout pins both options: Create because neither branch exists yet, and Keep
// because dropping it resets the tree and discards the changes the work branch exists to carry.
// testHead is where a mocked run's HEAD stands when it creates its update branch.
var testHead = plumbing.NewHashReference(plumbing.HEAD, plumbing.NewHash("1a2b3c"))

// expectUpdateBranch lets a run create its update branch at HEAD, which a run that gets that far
// does after committing the updated dependencies.
func expectUpdateBranch(repository *MockGitRepository) {
	repository.EXPECT().Head().Return(testHead, nil).Maybe()
	repository.EXPECT().SetBranch(mock.Anything, testHead.Hash()).Return(nil).Maybe()
	repository.EXPECT().RemoveBranch(mock.Anything).Return(nil).Maybe()
}

func TestStageScaffoldChanges(t *testing.T) {
	logger := zap.NewNop()

	newComposer := func(webroot string) *MockComposer {
		c := NewMockComposer(t)
		c.EXPECT().GetConfig(mock.Anything, "/project", "extra.drupal-scaffold.locations.web-root").
			Return(webroot, nil).Maybe()
		return c
	}

	t.Run("stages tracked changes and leaves untracked files alone", func(t *testing.T) {
		// A core update rewrites the scaffolded files; vendor/ is untracked and belongs to nobody.
		worktree := NewMockWorktree(t)
		worktree.EXPECT().Status().Return(git.Status{
			"web/.htaccess":       {Worktree: git.Modified},
			"web/robots.txt":      {Worktree: git.Modified},
			"web/core/foo.php":    {Worktree: git.Untracked},
			"vendor/autoload.php": {Worktree: git.Untracked},
			"README.md":           {Worktree: git.Unmodified},
		}, nil)

		var staged []string
		worktree.EXPECT().Add(mock.Anything).RunAndReturn(func(path string) (plumbing.Hash, error) {
			staged = append(staged, path)
			return plumbing.ZeroHash, nil
		})

		ws := &Runner{
			logger:   logger,
			config:   internal.Config{Sites: []string{"default"}},
			composer: newComposer("web/"),
		}

		require.NoError(t, ws.stageScaffoldChanges(context.Background(), "/project", worktree))
		// Sorted, so the commit a run produces does not depend on map iteration order.
		assert.Equal(t, []string{"web/.htaccess", "web/robots.txt"}, staged)
	})

	t.Run("stages nothing outside the web root", func(t *testing.T) {
		// Regression: staging every tracked change swept the config export into the
		// shared-code commit, still carrying the installer's sqlite entry, and the next run
		// then refused to install from it. Those trees belong to the per-site export.
		worktree := NewMockWorktree(t)
		worktree.EXPECT().Status().Return(git.Status{
			"web/robots.txt":                 {Worktree: git.Modified},
			"config/sync/core.extension.yml": {Worktree: git.Modified},
			"config/second/system.site.yml":  {Worktree: git.Modified},
			"translations/drupal.de.po":      {Worktree: git.Modified},
			"webhooks/notes.md":              {Worktree: git.Modified},
		}, nil)

		var staged []string
		worktree.EXPECT().Add(mock.Anything).RunAndReturn(func(path string) (plumbing.Hash, error) {
			staged = append(staged, path)
			return plumbing.ZeroHash, nil
		})

		ws := &Runner{
			logger:   logger,
			config:   internal.Config{Sites: []string{"default"}},
			composer: newComposer("web/"),
		}

		require.NoError(t, ws.stageScaffoldChanges(context.Background(), "/project", worktree))
		// "webhooks/" is in the list because a prefix match on "web" rather than "web/" would
		// pull it in.
		assert.Equal(t, []string{"web/robots.txt"}, staged)
	})

	t.Run("propagates a status failure", func(t *testing.T) {
		worktree := NewMockWorktree(t)
		worktree.EXPECT().Status().Return(nil, errors.New("boom"))

		ws := &Runner{logger: logger, composer: newComposer("web/")}

		err := ws.stageScaffoldChanges(context.Background(), "/project", worktree)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to read worktree status")
	})

	t.Run("propagates a web root failure", func(t *testing.T) {
		worktree := NewMockWorktree(t)
		worktree.EXPECT().Status().Return(git.Status{"web/robots.txt": {Worktree: git.Modified}}, nil)

		composerCLI := NewMockComposer(t)
		composerCLI.EXPECT().GetConfig(mock.Anything, "/project", "extra.drupal-scaffold.locations.web-root").
			Return("", errors.New("no web root"))

		ws := &Runner{logger: logger, composer: composerCLI}

		err := ws.stageScaffoldChanges(context.Background(), "/project", worktree)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to determine web root")
	})

	t.Run("propagates a staging failure", func(t *testing.T) {
		worktree := NewMockWorktree(t)
		worktree.EXPECT().Status().Return(git.Status{"web/robots.txt": {Worktree: git.Modified}}, nil)
		worktree.EXPECT().Add("web/robots.txt").Return(plumbing.ZeroHash, errors.New("locked"))

		ws := &Runner{logger: logger, composer: newComposer("web/")}

		err := ws.stageScaffoldChanges(context.Background(), "/project", worktree)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to stage web/robots.txt")
	})
}

func TestRunFailsWhenScaffoldChangesCannotBeStaged(t *testing.T) {
	// Staging what the update rewrote is part of producing the commit, not a nicety: if it
	// fails, the run must fail rather than push a branch missing the scaffold changes.
	logger := zap.NewNop()
	installer := NewMockInstaller(t)
	repositoryService := NewMockRepository(t)
	vcsProvider := NewMockPlatform(t)
	repository := NewMockGitRepository(t)
	expectUpdateBranch(repository)
	mockComposer := NewMockComposer(t)
	expectVersionLookup(mockComposer)
	drush := NewMockDrush(t)

	config := internal.Config{
		RepositoryURL: "https://example.com/repo.git",
		Branch:        "main",
		Token:         "token",
		Clone:         true,
		Sites:         []string{"site1"},
	}

	worktree := NewMockWorktree(t)
	worktree.EXPECT().AddGlob(mock.Anything).Return(nil)
	worktree.EXPECT().Status().Return(nil, assert.AnError)

	installer.EXPECT().Install(anyCtx, testCheckout, "site1").Return(nil)

	repositoryService.EXPECT().CloneRepository(config.RepositoryURL, config.Branch, config.Token, "user", "mail").
		Return(repo.Checkout{Repository: repository, Worktree: worktree, Path: testCheckout, Author: testAuthor}, nil)
	repositoryService.EXPECT().IsShallowClone(testCheckout).Return(false, nil)
	repository.EXPECT().Reference(mock.Anything, mock.Anything).Return(nil, plumbing.ErrReferenceNotFound).Maybe()
	vcsProvider.EXPECT().GetUser(mock.Anything).Return("user", "mail")

	mockComposer.EXPECT().CheckPlatformReqs(anyCtx, testCheckout).Return("", nil)
	mockComposer.EXPECT().Install(anyCtx, testCheckout).Return(nil)
	mockComposer.EXPECT().Update(anyCtx, testCheckout, mock.Anything, mock.Anything, false, false).
		Return([]composer.PackageChange{{Package: "drupal/core", From: "9.0.0", To: "9.1.0"}}, nil)
	mockComposer.EXPECT().GetLockHash(testCheckout).Return("dummy-hash", nil)
	repositoryService.EXPECT().BranchExists(repository, "update-dummy-hash", "token").Return(false, nil)

	workflowService := New(logger, config, drush, vcsProvider, repositoryService, installer, mockComposer)
	err := workflowService.Run(context.Background(), nil)

	require.ErrorIs(t, err, assert.AnError)
	assert.Contains(t, err.Error(), "failed to read worktree status")
}

type runKey struct{}

func TestRunHandsAddonsTheRunsContextAndCommitter(t *testing.T) {
	installer := NewMockInstaller(t)
	repositoryService := NewMockRepository(t)
	vcsProvider := NewMockPlatform(t)
	repository := NewMockGitRepository(t)
	expectUpdateBranch(repository)
	mockComposer := NewMockComposer(t)
	expectVersionLookup(mockComposer)
	drush := NewMockDrush(t)

	config := internal.Config{RepositoryURL: "https://example.com/repo.git", Branch: "main", Token: "token", Clone: true, Sites: []string{"site1"}, DryRun: true}

	worktree := NewMockWorktree(t)
	worktree.EXPECT().Add("config/sync").Return(plumbing.ZeroHash, nil).Maybe()
	worktree.EXPECT().AddGlob(mock.Anything).Return(nil)
	worktree.EXPECT().Status().Return(git.Status{}, nil).Once()
	worktree.EXPECT().Commit("Update composer.json and composer.lock", mock.Anything).Return(plumbing.ZeroHash, nil)
	// What an addon committing from post-code-update reaches: the run's own worktree.
	worktree.EXPECT().Add("web/modules/custom/a.php").Return(plumbing.ZeroHash, nil)
	worktree.EXPECT().Status().Return(git.Status{"web/modules/custom/a.php": {Staging: git.Modified}}, nil).Once()
	worktree.EXPECT().Commit("Remove deprecations", mock.Anything).Return(plumbing.ZeroHash, nil)
	worktree.EXPECT().Status().Return(git.Status{}, nil).Once()

	vcsProvider.EXPECT().GetUser(mock.Anything).Return("user", "mail")
	repositoryService.EXPECT().CloneRepository(config.RepositoryURL, config.Branch, config.Token, "user", "mail").Return(repo.Checkout{Repository: repository, Worktree: worktree, Path: testCheckout, Author: testAuthor}, nil)
	repositoryService.EXPECT().IsShallowClone(testCheckout).Return(false, nil)
	repository.EXPECT().Reference(mock.Anything, mock.Anything).Return(nil, plumbing.ErrReferenceNotFound)
	mockComposer.EXPECT().CheckPlatformReqs(anyCtx, testCheckout).Return("", nil)
	mockComposer.EXPECT().Install(anyCtx, testCheckout).Return(nil)
	mockComposer.EXPECT().Update(anyCtx, testCheckout, mock.Anything, mock.Anything, false, false).
		Return([]composer.PackageChange{{Action: "Upgrade", Package: "drupal/core", From: "9.0.0", To: "9.1.0"}}, nil)
	mockComposer.EXPECT().GetLockHash(testCheckout).Return("dummy-hash", nil)
	installer.EXPECT().Install(anyCtx, testCheckout, "site1").Return(nil)
	installer.EXPECT().ConfigureDatabase(anyCtx, testCheckout, "site1").Return(nil)
	installer.EXPECT().RestoreSettings(anyCtx, testCheckout, "site1").Return(nil)
	drush.EXPECT().UpdateSite(anyCtx, testCheckout, "site1").Return(nil)
	drush.EXPECT().ConfigResave(anyCtx, testCheckout, "site1").Return(nil)
	drush.EXPECT().ExportConfiguration(anyCtx, testCheckout, "site1").Return(nil)
	drush.EXPECT().GetConfigSyncDir(anyCtx, testCheckout, "site1", true).Return("config/sync", nil)

	ctx := context.WithValue(t.Context(), runKey{}, "this run")
	seen := map[string]bool{}
	carriesRun := func(name string, e interface{ Context() context.Context }) {
		seen[name] = e.Context().Value(runKey{}) == "this run"
	}
	observer := hookAddon{Hooks{
		PreComposerUpdate: Propose(Change, func(e *PreComposerUpdateEvent) (UpdatePlan, error) {
			carriesRun("pre-composer-update", e)
			return UpdatePlan{}, nil
		}),
		PostComposerUpdate: On(Change, func(e *PostComposerUpdateEvent) error {
			carriesRun("post-composer-update", e)
			return nil
		}),
		PostCodeUpdate: On(Change, func(e *PostCodeUpdateEvent) error {
			carriesRun("post-code-update", e)
			_, err := e.Commit("Remove deprecations", "web/modules/custom/a.php")
			return err
		}),
		PreSiteUpdate: On(Change, func(e *PreSiteUpdateEvent) error {
			carriesRun("pre-site-update", e)
			return nil
		}),
		PostSiteUpdate: On(Change, func(e *PostSiteUpdateEvent) error {
			carriesRun("post-site-update", e)
			return nil
		}),
	}}

	err := New(zap.NewNop(), config, drush, vcsProvider, repositoryService, installer, mockComposer).Run(ctx, []Addon{observer})

	require.NoError(t, err)
	assert.Equal(t, map[string]bool{
		"pre-composer-update": true, "post-composer-update": true, "post-code-update": true,
		"pre-site-update": true, "post-site-update": true,
	}, seen, "every addon event carries the run's context")
}

// yourTurn is a turn that has already come, for a site updated on its own.
func yourTurn() <-chan struct{} {
	turn := make(chan struct{})
	close(turn)
	return turn
}
