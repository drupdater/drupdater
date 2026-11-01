package workflow

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/drupdater/drupdater/internal/codehosting"
	"github.com/drupdater/drupdater/internal/composer"
	"github.com/drupdater/drupdater/internal/config"
	"github.com/drupdater/drupdater/internal/golden"
	"github.com/drupdater/drupdater/internal/repo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// captureDescription records the description the run opens its merge request with.
func captureDescription(h *runHarness, mr codehosting.MergeRequest, description *string) {
	h.vcsProvider.EXPECT().CreateMergeRequest(anyCtx, mock.Anything, mock.Anything, "update-dummy-hash", h.config.Branch).
		RunAndReturn(func(_ context.Context, _ string, got string, _ string, _ string) (codehosting.MergeRequest, error) {
			*description = got
			return mr, nil
		})
}

func TestRunPublishesTheDependencyUpdateDescription(t *testing.T) {
	h := newRunHarness(t, false)
	h.expectPublish()
	// Captured rather than matched on: as an expectation a changed description fails as an
	// unexpected call, and the diff that would say what changed is never printed.
	var description string
	captureDescription(h, codehosting.MergeRequest{}, &description)

	require.NoError(t, h.run(t))

	golden.Assert(t, "testdata/dependency_update.md", description)
}

func TestRunPushesTheUpdateBranchWithTheToken(t *testing.T) {
	h := newRunHarness(t, false)
	h.expectPublish()
	h.repository.EXPECT().Push(anyCtx, "update-dummy-hash", "s3cret").Return(nil).Once()

	require.NoError(t, h.run(t))
}

// Regression: publishWork must run on the outer context. The errgroup's is cancelled as soon as
// g.Wait() returns, so CreateMergeRequest would see a dead one.
func TestRunPublishUsesLiveContext(t *testing.T) {
	h := newRunHarness(t, false)
	h.expectPublish()
	var publishCtxErr error
	h.vcsProvider.EXPECT().CreateMergeRequest(anyCtx, mock.Anything, mock.Anything, mock.Anything, "main").
		RunAndReturn(func(ctx context.Context, _, _, _, _ string) (codehosting.MergeRequest, error) {
			publishCtxErr = ctx.Err()
			return codehosting.MergeRequest{}, nil
		})

	require.NoError(t, h.run(t))
	require.NoError(t, publishCtxErr, "publishWork must receive a live (non-cancelled) context")
}

// A failing site update must propagate the error and never create a branch, push or open an MR.
// Only the calls up to the failure are declared, so any later one fails the test.
func TestRunSiteFailureDoesNotPublish(t *testing.T) {
	h := newRunHarness(t, false)
	h.expectUpdate()
	h.repository.EXPECT().BranchExists(anyCtx, mock.Anything).Return(false, nil)
	h.repository.EXPECT().RemoteBranchExists(anyCtx, mock.Anything, mock.Anything).Return(false, nil)
	h.worktree.EXPECT().Commit(anyCtx, mock.Anything, mock.Anything).Return(nil)
	h.worktree.EXPECT().StageGlob(anyCtx, mock.Anything).Return(nil)
	h.worktree.EXPECT().Unstaged(anyCtx).Return(nil, nil).Maybe()
	h.worktree.EXPECT().Staged(anyCtx).Return(nil, nil).Maybe()
	h.installer.EXPECT().Install(anyCtx, testCheckout, "site1").Return(nil)
	h.installer.EXPECT().ConfigureDatabase(anyCtx, testCheckout, "site1").Return(nil)
	h.installer.EXPECT().RestoreSettings(anyCtx, testCheckout, "site1").Return(nil)
	updateErr := errors.New("drush updatedb crashed")
	h.drush.EXPECT().Facts(anyCtx, testCheckout, "site1").Return(syncFacts(testCheckout+"/config/sync"), nil)
	h.drush.EXPECT().UpdateSite(anyCtx, testCheckout, "site1").Return(updateErr)

	require.ErrorIs(t, h.run(t), updateErr)
}

// A run-level timeout must cancel the workflow context and surface as a deadline error rather
// than hanging.
func TestRunTimeout(t *testing.T) {
	h := newRunHarness(t, false)
	h.config.Timeout = 20 * time.Millisecond
	// Both phases block on the context, simulating a wedged subprocess.
	h.composer.EXPECT().Install(anyCtx, testCheckout).RunAndReturn(func(ctx context.Context, _ string) error {
		<-ctx.Done()
		return ctx.Err()
	})
	h.composer.EXPECT().Update(anyCtx, testCheckout, mock.Anything, mock.Anything, false, false).
		RunAndReturn(func(ctx context.Context, _ string, _ []string, _ []string, _ bool, _ bool) ([]composer.PackageChange, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		}).Maybe()

	require.ErrorIs(t, h.run(t), context.DeadlineExceeded)
}

// If the runtime PHP does not satisfy the project's platform requirements, the run aborts
// before composer update.
func TestRunPlatformReqsFail(t *testing.T) {
	h := newRunHarness(t, false)
	h.composer.EXPECT().CheckPlatformReqs(anyCtx, testCheckout).
		Return("php 8.1.0 failed", errors.New("unmet platform requirements"))
	// The baseline site install may start before the cancel propagates.
	h.installer.EXPECT().Install(anyCtx, testCheckout, "site1").Return(nil).Maybe()

	require.ErrorContains(t, h.run(t), "PHP platform requirements not satisfied")
}

func TestRunAbortsWithoutTheAgent(t *testing.T) {
	h := newRunHarness(t, false)
	h.composer.EXPECT().CheckAgent(anyCtx, testCheckout).Return(errors.New("command drupdater:handshake not defined"))

	err := h.run(t)

	require.EqualError(t, err, "drupdater agent installed in Composer: command drupdater:handshake not defined "+
		"(run drupdater from its Docker image, which installs the agent)")
	// The harness allows both, so their absence has to be asserted.
	h.composer.AssertNumberOfCalls(t, "CheckPlatformReqs", 0)
	h.composer.AssertNumberOfCalls(t, "Install", 0)
}

// An update that changes nothing aborts before installing any site.
func TestRunNoChanges(t *testing.T) {
	h := newRunHarness(t, false)
	h.composer.EXPECT().Update(anyCtx, testCheckout, mock.Anything, mock.Anything, false, false).Return([]composer.PackageChange{}, nil)

	_, aborted := errors.AsType[AbortError](h.run(t))
	require.True(t, aborted)
}

// A --security run on a site without advisories aborts in pre-composer-update. It must do so
// before the baseline install, the most expensive step of a run.
func TestRunAbortedByAnAddonInstallsNoSite(t *testing.T) {
	h := newRunHarness(t, false)
	h.withAddons(hookAddon{Hooks{PreComposerUpdate: propose(Prepare, func(*PreComposerUpdateEvent) (UpdatePlan, error) {
		return UpdatePlan{}, AbortError{Msg: "No security advisories found"}
	})}})

	err := h.run(t)

	_, aborted := errors.AsType[AbortError](err)
	require.True(t, aborted, "got %v", err)
}

func TestRunUpdatesAsTheAddonsPlanned(t *testing.T) {
	h := newRunHarness(t, false)
	h.composer.EXPECT().Update(anyCtx, testCheckout, []string{"drupal/core"}, []string{"drupal/core:10.2.0"}, true, false).
		Return(nil, nil)
	narrows := hookAddon{Hooks{PreComposerUpdate: propose(Prepare, func(*PreComposerUpdateEvent) (UpdatePlan, error) {
		return UpdatePlan{Only: []string{"drupal/core"}, MinimalChanges: true}, nil
	})}}
	pins := hookAddon{Hooks{PreComposerUpdate: propose(Change, func(*PreComposerUpdateEvent) (UpdatePlan, error) {
		return UpdatePlan{Keep: []Pin{{Package: "drupal/core", Version: "10.2.0"}}}, nil
	})}}
	h.withAddons(pins, narrows)

	err := h.run(t)

	_, aborted := errors.AsType[AbortError](err)
	require.True(t, aborted, "an update that changes nothing aborts; got %v", err)
}

// vendor/ must still hold the old code when the baseline is installed: the update is resolved
// into the lock first, and only installed once every site has its baseline database.
func TestRunInstallsSitesAtTheOldCode(t *testing.T) {
	h := newRunHarness(t, true)
	h.expectFullRun()
	core, logs := observer.New(zap.InfoLevel)
	h.logger = zap.New(core)

	var steps []string
	h.composer.EXPECT().Install(anyCtx, testCheckout).RunAndReturn(func(context.Context, string) error {
		steps = append(steps, "composer install")
		return nil
	})
	h.composer.EXPECT().Update(anyCtx, testCheckout, mock.Anything, mock.Anything, false, false).
		RunAndReturn(func(context.Context, string, []string, []string, bool, bool) ([]composer.PackageChange, error) {
			steps = append(steps, "composer update")
			return []composer.PackageChange{{Action: "Upgrade", Package: "drupal/core", From: "9.0.0", To: "9.1.0"}}, nil
		})
	h.installer.EXPECT().Install(anyCtx, testCheckout, "site1").RunAndReturn(func(context.Context, string, string) error {
		steps = append(steps, "site install")
		return nil
	})
	h.installer.EXPECT().ConfigureDatabase(anyCtx, testCheckout, "site1").RunAndReturn(func(context.Context, string, string) error {
		steps = append(steps, "site update")
		return nil
	})

	require.NoError(t, h.run(t))
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

// The update branch has to carry every commit, the exported configuration's included: the run
// commits on a detached HEAD, which a branch created any earlier would not follow.
func TestRunCreatesTheUpdateBranchAfterTheLastCommit(t *testing.T) {
	h := newRunHarness(t, true)
	h.config.Token = ""
	h.withoutPlatform = true
	h.expectFullRun()

	var events []string
	record := func(event string) { events = append(events, event) }
	// The exported configuration changed, so its commit is not skipped as empty.
	h.worktree.EXPECT().Staged(anyCtx).Return([]string{"config/sync/system.site.yml"}, nil)
	h.worktree.EXPECT().Commit(anyCtx, mock.Anything, mock.Anything).RunAndReturn(func(_ context.Context, message string, _ repo.Identity) error {
		record("commit: " + message)
		return nil
	})
	h.repository.EXPECT().SetBranch(anyCtx, "update-dummy-hash", testHead).RunAndReturn(func(context.Context, string, string) error {
		record("branch")
		return nil
	})

	require.NoError(t, h.run(t))

	assert.Equal(t, []string{
		"commit: Update composer.json and composer.lock",
		"commit: Update configuration site1",
		"branch",
	}, events)
}

func TestRunBranchAlreadyExists(t *testing.T) {
	h := newRunHarness(t, false)
	h.expectUpdate()
	h.repository.EXPECT().BranchExists(anyCtx, "update-dummy-hash").Return(false, nil)
	h.repository.EXPECT().RemoteBranchExists(anyCtx, "update-dummy-hash", "s3cret").Return(true, nil)

	err := h.run(t)

	_, aborted := errors.AsType[AbortError](err)
	require.True(t, aborted, "got %v", err)
	assert.ErrorContains(t, err, "already exists")
}

// Regression: a local ref left by a prior run of the same content hash must abort with the clean
// AbortError, not git's raw message from creating the branch later. RemoteBranchExists is left
// undeclared: the remote check must not run once the local ref is found.
func TestRunLocalBranchAlreadyExists(t *testing.T) {
	h := newRunHarness(t, false)
	h.expectUpdate()
	h.repository.EXPECT().BranchExists(anyCtx, "update-dummy-hash").Return(true, nil)

	err := h.run(t)

	_, aborted := errors.AsType[AbortError](err)
	require.True(t, aborted, "got %v", err)
	assert.ErrorContains(t, err, "already exists locally")
}

// A lookup failure that is not "not found" (e.g. a corrupt local ref) must surface as an error
// rather than being silently treated as "no local branch".
func TestRunLocalBranchLookupError(t *testing.T) {
	h := newRunHarness(t, false)
	h.expectUpdate()
	lookupErr := errors.New("corrupt ref")
	h.repository.EXPECT().BranchExists(anyCtx, mock.Anything).Return(false, lookupErr)

	require.ErrorIs(t, h.run(t), lookupErr)
}

func TestRunBranchExistsError(t *testing.T) {
	h := newRunHarness(t, false)
	h.expectUpdate()
	branchErr := errors.New("git remote unreachable")
	h.repository.EXPECT().BranchExists(anyCtx, mock.Anything).Return(false, nil)
	h.repository.EXPECT().RemoteBranchExists(anyCtx, mock.Anything, mock.Anything).Return(false, branchErr)

	err := h.run(t)

	require.ErrorContains(t, err, "failed to check if branch exists")
	require.ErrorIs(t, err, branchErr)
}

func TestRunProjectReadErrorAfterTheUpdate(t *testing.T) {
	h := newRunHarness(t, false)
	h.composer.EXPECT().Update(anyCtx, testCheckout, mock.Anything, mock.Anything, false, false).
		Return([]composer.PackageChange{{Package: "drupal/core", From: "9.0.0", To: "9.1.0"}}, nil)
	hashErr := errors.New("composer.lock not found")
	h.composer.EXPECT().Project(anyCtx, testCheckout).Return(composer.Project{}, hashErr)

	require.ErrorIs(t, h.run(t), hashErr)
}

// A --dry-run never pushes, so ensureUpdateBranchAvailable skips the remote branch check, which
// the harness leaves undeclared on a dry run. Reaching it used to fail without a token.
func TestRunWithDryRun(t *testing.T) {
	h := newRunHarness(t, true)
	h.expectFullRun()

	require.NoError(t, h.run(t))
}

// Regression for issue #167: a token-free dry run has no VCS platform, and Run must not
// dereference nil to get the commit identity.
func TestRunCheckoutDryRunWithoutPlatform(t *testing.T) {
	h := newRunHarness(t, true)
	h.config = config.Config{WorkingDir: "/checkout", Branch: "main", Sites: []string{"site1"}, DryRun: true}
	h.withoutPlatform = true
	h.expectFullRun()

	require.NoError(t, h.run(t))
}

func TestRunFailsWhenComposerInstallFails(t *testing.T) {
	h := newRunHarness(t, true)
	h.composer.EXPECT().Install(anyCtx, testCheckout).Return(assert.AnError)

	err := h.run(t)

	require.ErrorIs(t, err, assert.AnError)
	require.ErrorContains(t, err, "failed to run composer install")
	assert.Equal(t, "composer install", h.got.FailedPhase)
}

func TestRunConfigResaveError(t *testing.T) {
	h := newRunHarness(t, false)
	h.expectFullRun()
	resaveErr := errors.New("drush config:resave failed")
	h.drush.EXPECT().ConfigResave(anyCtx, testCheckout, "site1").Return(resaveErr)

	require.ErrorIs(t, h.run(t), resaveErr)
}

func TestRunExportConfigurationError(t *testing.T) {
	h := newRunHarness(t, false)
	h.expectFullRun()
	exportErr := errors.New("drush config:export failed")
	h.drush.EXPECT().ExportConfiguration(anyCtx, testCheckout, "site1").Return(exportErr)

	require.ErrorIs(t, h.run(t), exportErr)
}

func TestRunFireEventError(t *testing.T) {
	h := newRunHarness(t, false)
	// The baseline site install may run before the hook's failure cancels the run.
	h.expectFullRun()
	h.withAddons(hookAddon{Hooks{PreComposerUpdate: propose(Change, func(*PreComposerUpdateEvent) (UpdatePlan, error) {
		return UpdatePlan{}, errors.New("audit unavailable")
	})}})

	err := h.run(t)

	require.ErrorContains(t, err, "pre-composer-update hook failed")
	require.ErrorContains(t, err, "audit unavailable")
}

func TestPublishWorkPushFails(t *testing.T) {
	h := newRunHarness(t, false)
	h.expectFullRun()
	pushErr := errors.New("authentication failed")
	h.repository.EXPECT().Push(anyCtx, mock.Anything, mock.Anything).Return(pushErr)

	require.ErrorIs(t, h.run(t), pushErr)
}

// When CreateMergeRequest fails, publishWork deletes the remote branch it just pushed so the
// remote is left clean for the next run.
func TestPublishWorkDeletesBranchOnMRFailure(t *testing.T) {
	h := newRunHarness(t, false)
	h.expectPublish()
	mrErr := errors.New("API rate limit exceeded")
	h.vcsProvider.EXPECT().CreateMergeRequest(anyCtx, mock.Anything, mock.Anything, mock.Anything, "main").Return(codehosting.MergeRequest{}, mrErr)
	h.vcsProvider.EXPECT().DeleteBranch(anyCtx, "update-dummy-hash").Return(nil)

	require.ErrorIs(t, h.run(t), mrErr)
}

// The cleanup is best-effort: its failure is only logged, the MR creation error is returned.
func TestPublishWorkLogsWarningWhenDeleteBranchFails(t *testing.T) {
	h := newRunHarness(t, false)
	h.expectPublish()
	mrErr := errors.New("API rate limit exceeded")
	deleteErr := errors.New("permission denied")
	h.vcsProvider.EXPECT().CreateMergeRequest(anyCtx, mock.Anything, mock.Anything, mock.Anything, "main").Return(codehosting.MergeRequest{}, mrErr)
	h.vcsProvider.EXPECT().DeleteBranch(anyCtx, mock.Anything).Return(deleteErr)

	err := h.run(t)

	require.ErrorIs(t, err, mrErr)
	require.NotErrorIs(t, err, deleteErr)
}

func TestRunWithAutoMerge(t *testing.T) {
	h := newRunHarness(t, false)
	h.config.RunTypes = config.RunTypesConfig{Normal: config.RunTypeConfig{AutoMerge: true}}
	h.expectPublish()
	createdMR := codehosting.MergeRequest{ID: 42, URL: "http://example.com/mr/42"}
	var description string
	captureDescription(h, createdMR, &description)
	h.vcsProvider.EXPECT().EnableAutoMerge(anyCtx, createdMR).Return(nil)

	require.NoError(t, h.run(t))
	golden.Assert(t, "testdata/dependency_update.md", description)
}

// Auto-merge is a convenience on top of work that already succeeded: the branch is pushed and
// the MR exists. A failure here must be logged as a warning, not fail the run.
func TestRunAutoMergeError(t *testing.T) {
	h := newRunHarness(t, false)
	h.config.RunTypes = config.RunTypesConfig{Normal: config.RunTypeConfig{AutoMerge: true}}
	core, logs := observer.New(zap.WarnLevel)
	h.logger = zap.New(core)
	h.expectPublish()
	createdMR := codehosting.MergeRequest{ID: 42, URL: "http://example.com/mr/42"}
	h.vcsProvider.EXPECT().CreateMergeRequest(anyCtx, mock.Anything, mock.Anything, mock.Anything, "main").Return(createdMR, nil)
	autoMergeErr := errors.New("auto-merge not allowed")
	h.vcsProvider.EXPECT().EnableAutoMerge(anyCtx, createdMR).Return(autoMergeErr)

	require.NoError(t, h.run(t), "a failed auto-merge must not fail the run — the MR was created")

	warnings := logs.FilterMessage("failed to enable auto merge").All()
	require.Len(t, warnings, 1, "the failure must still be reported to the operator")
	assert.Contains(t, warnings[0].ContextMap()["error"], autoMergeErr.Error())
}

// EnableAutoMerge is left undeclared, so calling it with auto-merge off fails the test.
func TestRunAutoMergeSkippedWhenDisabled(t *testing.T) {
	h := newRunHarness(t, false)
	h.expectPublish()

	require.NoError(t, h.run(t))
}

func TestASecondRunLeavesTheFirstRunsBranchAlone(t *testing.T) {
	h := newRunHarness(t, true)
	h.expectFullRun()
	// The second run fails resolving its update, before it creates a branch of its own.
	runs := 0
	h.withAddons(hookAddon{Hooks{PreComposerUpdate: propose(Prepare, func(*PreComposerUpdateEvent) (UpdatePlan, error) {
		runs++
		if runs == 2 {
			return UpdatePlan{}, assert.AnError
		}
		return UpdatePlan{}, nil
	})}})
	runner := h.runner()

	require.NoError(t, runner.Run(t.Context(), h.addons))
	require.ErrorIs(t, runner.Run(t.Context(), h.addons), assert.AnError)

	h.repository.AssertCalled(t, "SetBranch", mock.Anything, "update-dummy-hash", testHead)
	h.repository.AssertNumberOfCalls(t, "RemoveBranch", 0)
}

func TestRunRemovesTheBranchWhenPublishingFails(t *testing.T) {
	h := newRunHarness(t, false)
	h.expectFullRun()
	h.repository.EXPECT().Push(anyCtx, mock.Anything, mock.Anything).Return(assert.AnError)

	err := h.run(t)

	require.ErrorContains(t, err, "failed to push changes")
	assert.Equal(t, "publish", h.got.FailedPhase)
	h.repository.AssertCalled(t, "RemoveBranch", mock.Anything, "update-dummy-hash")
}

func TestRunRemovesNoBranchWhenCreatingItFails(t *testing.T) {
	h := newRunHarness(t, true)
	h.expectFullRun()
	h.repository.EXPECT().SetBranch(anyCtx, "update-dummy-hash", testHead).Return(assert.AnError)

	err := h.run(t)

	require.ErrorContains(t, err, "failed to create branch update-dummy-hash")
	assert.Equal(t, "site update", h.got.FailedPhase)
	h.repository.AssertNumberOfCalls(t, "RemoveBranch", 0)
}

// checkoutModeRun is a checkout-mode run that gets all the way to publishing, working in a
// worktree at worktreePath of the checkout at source.
func checkoutModeRun(t *testing.T, source, worktreePath string) *runHarness {
	t.Helper()
	h := newRunHarness(t, false)
	h.config = config.Config{Branch: "main", Token: "token", WorkingDir: source, Sites: []string{"site1"}}
	h.path = worktreePath
	h.expectPublish()
	return h
}

// Without --clone the run works in a worktree of the checkout, never in the checkout itself, and
// removes the worktree when done. The update branch is all it leaves behind. CloneRepository is
// left undeclared, so a clone fails the test.
func TestRunWorksInAWorktreeOfTheCheckout(t *testing.T) {
	source, worktreePath := t.TempDir(), t.TempDir()
	h := checkoutModeRun(t, source, worktreePath)
	h.repoSvc.EXPECT().RemoveWorktree(anyCtx, source, worktreePath).Return(nil).Once()

	require.NoError(t, h.run(t))

	h.repository.AssertCalled(t, "SetBranch", mock.Anything, "update-dummy-hash", testHead)
	h.repository.AssertNumberOfCalls(t, "RemoveBranch", 0)
}

// A worktree that will not go away is a leftover temporary directory, not a failed update.
func TestRunOnlyWarnsWhenItsWorktreeCannotBeRemoved(t *testing.T) {
	source, worktreePath := t.TempDir(), t.TempDir()
	h := checkoutModeRun(t, source, worktreePath)
	h.repoSvc.EXPECT().RemoveWorktree(anyCtx, source, worktreePath).Return(assert.AnError)
	core, logs := observer.New(zap.WarnLevel)
	h.logger = zap.New(core)

	require.NoError(t, h.run(t))

	warnings := logs.FilterMessage("failed to remove the run's worktree").All()
	require.Len(t, warnings, 1)
	assert.Equal(t, worktreePath, warnings[0].ContextMap()["path"])
}

// Cancelling the run must not stop it from removing its worktree.
func TestRunRemovesItsWorktreeEvenWhenCancelled(t *testing.T) {
	source := t.TempDir()
	installer := NewMockInstaller(t)
	repositoryService := NewMockGitService(t)
	mockComposer := NewMockComposer(t)
	expectVersionLookup(mockComposer)
	ctx, cancel := context.WithCancel(t.Context())

	repositoryService.EXPECT().AddWorktree(anyCtx, source, repo.Identity{}).
		Return(repo.Checkout{Repository: NewMockGitRepository(t), Worktree: NewMockWorktree(t), Path: testCheckout, Author: testAuthor}, nil)
	repositoryService.EXPECT().IsShallowClone(anyCtx, testCheckout).RunAndReturn(func(context.Context, string) (bool, error) {
		cancel()
		return false, context.Canceled
	})
	repositoryService.EXPECT().RemoveWorktree(mock.MatchedBy(func(ctx context.Context) bool { return ctx.Err() == nil }), source, testCheckout).Return(nil)

	config := config.Config{Branch: "main", WorkingDir: source, Sites: []string{"site1"}, DryRun: true}
	err := New(zap.NewNop(), config, NewMockDrush(t), nil, repositoryService, installer, mockComposer).Run(ctx, nil)

	require.Error(t, err)
}
