package workflow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"testing/synctest"

	"github.com/drupdater/drupdater/internal/buildinfo"
	"github.com/drupdater/drupdater/internal/composer"
	"github.com/drupdater/drupdater/internal/config"
	"github.com/drupdater/drupdater/internal/drush"
	"github.com/drupdater/drupdater/internal/repo"
	"github.com/drupdater/drupdater/internal/report"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestUpdateSharedCodeFailures(t *testing.T) {
	newRunner := func(t *testing.T, hooks Hooks) (*run, *MockComposer, *MockWorktree) {
		t.Helper()
		mockComposer := NewMockComposer(t)
		worktree := NewMockWorktree(t)
		return testRun(services{logger: zap.NewNop(), composer: mockComposer}, repo.Checkout{Worktree: worktree, Path: testCheckout}, hookAddon{hooks}), mockComposer, worktree
	}

	t.Run("installing the resolved lock", func(t *testing.T) {
		ws, mockComposer, _ := newRunner(t, Hooks{})
		mockComposer.EXPECT().Install(anyCtx, testCheckout).Return(assert.AnError)

		err := ws.updateSharedCode(t.Context())
		require.ErrorIs(t, err, assert.AnError)
		assert.Contains(t, err.Error(), "failed to install updated dependencies")
	})

	t.Run("post-composer-update", func(t *testing.T) {
		ws, mockComposer, _ := newRunner(t, Hooks{PostComposerUpdate: On(Change, failing[*PostComposerUpdateEvent])})
		mockComposer.EXPECT().Install(anyCtx, testCheckout).Return(nil)

		err := ws.updateSharedCode(t.Context())
		require.ErrorIs(t, err, assert.AnError)
		assert.Contains(t, err.Error(), "post-composer-update hook failed")
	})

	t.Run("staging composer files", func(t *testing.T) {
		ws, mockComposer, worktree := newRunner(t, Hooks{})
		mockComposer.EXPECT().Install(anyCtx, testCheckout).Return(nil)
		worktree.EXPECT().StageGlob(anyCtx, "composer.*").Return(assert.AnError)

		err := ws.updateSharedCode(t.Context())
		require.ErrorIs(t, err, assert.AnError)
		assert.Contains(t, err.Error(), "failed to add composer.* files")
	})

	t.Run("committing", func(t *testing.T) {
		ws, mockComposer, worktree := newRunner(t, Hooks{})
		mockComposer.EXPECT().Install(anyCtx, testCheckout).Return(nil)
		worktree.EXPECT().StageGlob(anyCtx, "composer.*").Return(nil)
		worktree.EXPECT().Unstaged(anyCtx).Return(nil, nil)
		worktree.EXPECT().Commit(anyCtx, mock.Anything, mock.Anything).Return(assert.AnError)

		err := ws.updateSharedCode(t.Context())
		require.ErrorIs(t, err, assert.AnError)
		assert.Contains(t, err.Error(), "failed to commit composer.json and composer.lock")
	})

	t.Run("post-code-update", func(t *testing.T) {
		ws, mockComposer, worktree := newRunner(t, Hooks{PostCodeUpdate: On(Change, failing[*PostCodeUpdateEvent])})
		mockComposer.EXPECT().Install(anyCtx, testCheckout).Return(nil)
		worktree.EXPECT().StageGlob(anyCtx, "composer.*").Return(nil)
		worktree.EXPECT().Unstaged(anyCtx).Return(nil, nil)
		worktree.EXPECT().Commit(anyCtx, mock.Anything, mock.Anything).Return(nil)

		err := ws.updateSharedCode(t.Context())
		require.ErrorIs(t, err, assert.AnError)
		assert.Contains(t, err.Error(), "post-code-update hook failed")
	})

}

func TestCreateUpdateBranch(t *testing.T) {
	newRun := func(t *testing.T) (*run, *MockGitRepository) {
		t.Helper()
		repository := NewMockGitRepository(t)
		return testRun(services{logger: zap.NewNop()}, repo.Checkout{Repository: repository, Path: testCheckout}), repository
	}

	t.Run("points the branch at HEAD", func(t *testing.T) {
		ws, repository := newRun(t)
		repository.EXPECT().Head(anyCtx).Return(testHead, nil)
		repository.EXPECT().SetBranch(anyCtx, "update-x", testHead).Return(nil)

		require.NoError(t, ws.createUpdateBranch(t.Context(), "update-x"))
	})

	t.Run("reading HEAD fails", func(t *testing.T) {
		ws, repository := newRun(t)
		repository.EXPECT().Head(anyCtx).Return("", assert.AnError)

		err := ws.createUpdateBranch(t.Context(), "update-x")
		require.ErrorIs(t, err, assert.AnError)
		assert.Contains(t, err.Error(), "failed to read HEAD")
	})

	t.Run("creating the branch fails", func(t *testing.T) {
		ws, repository := newRun(t)
		repository.EXPECT().Head(anyCtx).Return(testHead, nil)
		repository.EXPECT().SetBranch(anyCtx, "update-x", testHead).Return(assert.AnError)

		err := ws.createUpdateBranch(t.Context(), "update-x")
		require.ErrorIs(t, err, assert.AnError)
		assert.Contains(t, err.Error(), "failed to create branch update-x")
	})
}

// The runner's own commit is signed explicitly, so no identity has to be in git config.
func TestUpdateSharedCodeSignsItsCommit(t *testing.T) {
	mockComposer := NewMockComposer(t)
	worktree := NewMockWorktree(t)
	ws := testRun(services{logger: zap.NewNop(), composer: mockComposer}, repo.Checkout{Worktree: worktree, Path: testCheckout, Author: testAuthor})
	mockComposer.EXPECT().Install(anyCtx, testCheckout).Return(nil)
	worktree.EXPECT().StageGlob(anyCtx, "composer.*").Return(nil)
	worktree.EXPECT().Unstaged(anyCtx).Return(nil, nil)
	worktree.EXPECT().Commit(anyCtx, "Update composer.json and composer.lock", testAuthor).Return(nil)

	require.NoError(t, ws.updateSharedCode(t.Context()))
}

func TestRemoveUpdateBranch(t *testing.T) {
	t.Run("nothing to remove before a branch was created", func(t *testing.T) {
		services{logger: zap.NewNop()}.removeUpdateBranch(t.Context(), NewMockGitRepository(t), "")
	})

	t.Run("a failure to remove only warns", func(t *testing.T) {
		core, logs := observer.New(zap.WarnLevel)
		repository := NewMockGitRepository(t)
		repository.EXPECT().RemoveBranch(anyCtx, "update-x").Return(assert.AnError)

		services{logger: zap.New(core)}.removeUpdateBranch(t.Context(), repository, "update-x")

		warnings := logs.FilterMessage("failed to remove the failed run's update branch").All()
		require.Len(t, warnings, 1)
		assert.Equal(t, "update-x", warnings[0].ContextMap()["branch"])
	})
}

func TestForEachSite(t *testing.T) {
	logger := zap.NewNop()
	sites := []string{"site1", "site2", "site3", "site4"}

	t.Run("bounds concurrency to config.Concurrency", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			ws := services{logger: logger, config: config.Config{Sites: sites, Concurrency: 2}}
			probe := newOccupancyProbe()

			err := ws.forEachSite(t.Context(), func(ctx context.Context, _ int, site string) error {
				return probe.probe(ctx, testCheckout, site)
			})

			require.NoError(t, err)
			assert.EqualValues(t, 2, probe.peak())
		})
	})

	t.Run("falls back to GOMAXPROCS(0) when Concurrency is unset", func(t *testing.T) {
		ws := services{logger: logger, config: config.Config{Sites: sites}}

		var calls atomic.Int32
		err := ws.forEachSite(context.Background(), func(_ context.Context, _ int, _ string) error {
			calls.Add(1)
			return nil
		})

		require.NoError(t, err)
		assert.EqualValues(t, len(sites), calls.Load())
	})

	t.Run("propagates the first error and cancels the rest", func(t *testing.T) {
		ws := services{logger: logger, config: config.Config{Sites: sites, Concurrency: 2}}
		boom := errors.New("boom")

		err := ws.forEachSite(context.Background(), func(_ context.Context, _ int, site string) error {
			if site == "site1" {
				return boom
			}
			return nil
		})

		require.ErrorIs(t, err, boom)
	})

	t.Run("starts no site queued behind the first error", func(t *testing.T) {
		ws := services{logger: logger, config: config.Config{Sites: sites, Concurrency: 1}}

		var started []string
		err := ws.forEachSite(context.Background(), func(_ context.Context, _ int, site string) error {
			started = append(started, site)
			return assert.AnError
		})

		require.ErrorIs(t, err, assert.AnError)
		assert.Equal(t, []string{"site1"}, started)
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

		ws := services{logger: logger, config: config.Config{Clone: true}}
		ws.cleanup(t.Context(), clone)

		assert.NoDirExists(t, clone)
		assert.DirExists(t, sibling) // a concurrent run's clone must survive
	})

	t.Run("clone mode refuses to remove the temp dir itself", func(t *testing.T) {
		// This asks cleanup() to delete the temp dir and relies on its guard to refuse, so
		// os.TempDir() is pointed at a sandbox: a broken guard must not wipe the real one.
		t.Setenv("TMPDIR", t.TempDir())

		ws := services{logger: logger, config: config.Config{Clone: true}}
		ws.cleanup(t.Context(), os.TempDir())
		assert.DirExists(t, os.TempDir())
	})

	t.Run("checkout mode removes the run's worktree and touches no directory itself", func(t *testing.T) {
		base := t.TempDir()
		work := filepath.Join(base, "checkout")
		require.NoError(t, os.MkdirAll(work, 0o755))
		repositoryService := NewMockGitService(t)
		repositoryService.EXPECT().RemoveWorktree(anyCtx, "/source", work).Return(nil)

		ws := services{logger: logger, git: repositoryService, config: config.Config{WorkingDir: "/source", Sites: []string{"site1"}}}
		ws.cleanup(t.Context(), work)

		assert.DirExists(t, work, "removing is the repository service's job, through git")
	})
}

func TestPostSiteUpdateSeesTheFactsReadAfterTheUpdateHooks(t *testing.T) {
	installer := NewMockInstaller(t)
	drushMock := NewMockDrush(t)
	committer := NewMockCommitter(t)
	var seen drush.Facts
	record := func(e *PostSiteUpdateEvent) error {
		seen = e.Facts()
		return nil
	}
	ws := testRun(services{logger: zap.NewNop(), installer: installer, drush: drushMock}, repo.Checkout{Path: "/project"},
		hookAddon{Hooks{PostSiteUpdate: On(Inspect, record)}})
	ws.committer = committer

	installer.EXPECT().ConfigureDatabase(anyCtx, "/project", "site1").Return(nil)
	drushMock.EXPECT().Facts(anyCtx, "/project", "site1").Return(syncFacts("/project/config/before"), nil)
	drushMock.EXPECT().UpdateSite(anyCtx, "/project", "site1").Return(nil)
	drushMock.EXPECT().ConfigResave(anyCtx, "/project", "site1").Return(nil)
	after := drush.Facts{ConfigSyncDirectory: "/project/config/after", Modules: []string{"locale_deploy"}}
	drushMock.EXPECT().LocalFacts(anyCtx, "/project", "site1").Return(after, nil)
	drushMock.EXPECT().ExportConfiguration(anyCtx, "/project", "site1").Return(nil)
	committer.EXPECT().Commit(anyCtx, "Update configuration site1", []string{"config/after"}).Return(true, nil)
	installer.EXPECT().RestoreSettings(anyCtx, "/project", "site1").Return(nil)

	require.NoError(t, updateSite1(t.Context(), ws))
	assert.Equal(t, after, seen)
}

func TestUpdateSiteRestoresSettings(t *testing.T) {
	newRunner := func(t *testing.T, addons ...Addon) (*run, *MockInstaller, *MockDrush) {
		t.Helper()
		installer := NewMockInstaller(t)
		drush := NewMockDrush(t)
		return testRun(services{logger: zap.NewNop(), installer: installer, drush: drush}, repo.Checkout{Path: "/project"}, addons...), installer, drush
	}

	t.Run("after a failed pre-site-update hook, which stops the update", func(t *testing.T) {
		ws, installer, drush := newRunner(t, hookAddon{Hooks{PreSiteUpdate: On(Inspect, failing[*PreSiteUpdateEvent])}})
		installer.EXPECT().ConfigureDatabase(anyCtx, "/project", "site1").Return(nil)
		drush.EXPECT().Facts(anyCtx, "/project", "site1").Return(syncFacts("/project/config/sync"), nil)
		installer.EXPECT().RestoreSettings(anyCtx, "/project", "site1").Return(nil).Once()

		err := updateSite1(t.Context(), ws)
		require.ErrorIs(t, err, assert.AnError)
		assert.Contains(t, err.Error(), "pre-site-update hook failed")
	})

	t.Run("after its facts cannot be read, which stops the update before any hook", func(t *testing.T) {
		ws, installer, drush := newRunner(t, hookAddon{Hooks{PreSiteUpdate: On(Inspect, failing[*PreSiteUpdateEvent])}})
		installer.EXPECT().ConfigureDatabase(anyCtx, "/project", "site1").Return(nil)
		drush.EXPECT().Facts(anyCtx, "/project", "site1").Return(syncFacts(""), assert.AnError)
		installer.EXPECT().RestoreSettings(anyCtx, "/project", "site1").Return(nil).Once()

		err := updateSite1(t.Context(), ws)
		require.ErrorIs(t, err, assert.AnError)
		assert.NotContains(t, err.Error(), "pre-site-update")
	})

	t.Run("after a failed post-site-update hook, which stops the export", func(t *testing.T) {
		ws, installer, drush := newRunner(t, hookAddon{Hooks{PostSiteUpdate: On(Change, failing[*PostSiteUpdateEvent])}})
		installer.EXPECT().ConfigureDatabase(anyCtx, "/project", "site1").Return(nil)
		drush.EXPECT().Facts(anyCtx, "/project", "site1").Return(syncFacts("/project/config/sync"), nil)
		drush.EXPECT().UpdateSite(anyCtx, "/project", "site1").Return(nil)
		drush.EXPECT().ConfigResave(anyCtx, "/project", "site1").Return(nil)
		drush.EXPECT().LocalFacts(anyCtx, "/project", "site1").Return(syncFacts("/project/config/sync"), nil)
		installer.EXPECT().RestoreSettings(anyCtx, "/project", "site1").Return(nil).Once()

		err := updateSite1(t.Context(), ws)
		require.ErrorIs(t, err, assert.AnError)
		assert.Contains(t, err.Error(), "post-site-update hook failed")
	})

	t.Run("after its facts cannot be read again, which stops the update before the export", func(t *testing.T) {
		ws, installer, drush := newRunner(t)
		installer.EXPECT().ConfigureDatabase(anyCtx, "/project", "site1").Return(nil)
		drush.EXPECT().Facts(anyCtx, "/project", "site1").Return(syncFacts("/project/config/sync"), nil)
		drush.EXPECT().UpdateSite(anyCtx, "/project", "site1").Return(nil)
		drush.EXPECT().ConfigResave(anyCtx, "/project", "site1").Return(nil)
		drush.EXPECT().LocalFacts(anyCtx, "/project", "site1").Return(syncFacts(""), assert.AnError)
		installer.EXPECT().RestoreSettings(anyCtx, "/project", "site1").Return(nil).Once()

		err := updateSite1(t.Context(), ws)
		require.ErrorIs(t, err, assert.AnError)
	})

	t.Run("after a failed update hook", func(t *testing.T) {
		ws, installer, drush := newRunner(t)
		installer.EXPECT().ConfigureDatabase(anyCtx, "/project", "site1").Return(nil)
		drush.EXPECT().Facts(anyCtx, "/project", "site1").Return(syncFacts("/project/config/sync"), nil)
		drush.EXPECT().UpdateSite(anyCtx, "/project", "site1").Return(assert.AnError)
		installer.EXPECT().RestoreSettings(anyCtx, "/project", "site1").Return(nil).Once()

		err := updateSite1(t.Context(), ws)
		require.ErrorIs(t, err, assert.AnError)
		assert.NotContains(t, err.Error(), "restore")
	})

	t.Run("with a context another site cancelled", func(t *testing.T) {
		ws, installer, drush := newRunner(t)
		ctx, cancel := context.WithCancel(t.Context())
		installer.EXPECT().ConfigureDatabase(anyCtx, "/project", "site1").Return(nil)
		drush.EXPECT().Facts(anyCtx, "/project", "site1").Return(syncFacts("/project/config/sync"), nil)
		drush.EXPECT().UpdateSite(anyCtx, "/project", "site1").RunAndReturn(func(context.Context, string, string) error {
			cancel()
			return context.Canceled
		})
		installer.EXPECT().RestoreSettings(mock.MatchedBy(func(ctx context.Context) bool { return ctx.Err() == nil }), "/project", "site1").Return(nil).Once()

		require.ErrorIs(t, updateSite1(ctx, ws), context.Canceled)
	})

	t.Run("a failed restore fails the site", func(t *testing.T) {
		ws, installer, drush := newRunner(t)
		installer.EXPECT().ConfigureDatabase(anyCtx, "/project", "site1").Return(nil)
		drush.EXPECT().Facts(anyCtx, "/project", "site1").Return(syncFacts("/project/config/sync"), nil)
		drush.EXPECT().UpdateSite(anyCtx, "/project", "site1").Return(nil)
		drush.EXPECT().ConfigResave(anyCtx, "/project", "site1").Return(nil)
		drush.EXPECT().LocalFacts(anyCtx, "/project", "site1").Return(syncFacts("/project/config/sync"), nil)
		drush.EXPECT().ExportConfiguration(anyCtx, "/project", "site1").Return(nil)
		committer := NewMockCommitter(t)
		committer.EXPECT().Commit(anyCtx, "Update configuration site1", []string{"config/sync"}).Return(true, nil)
		ws.committer = committer
		installer.EXPECT().RestoreSettings(anyCtx, "/project", "site1").Return(assert.AnError)

		err := updateSite1(t.Context(), ws)
		require.ErrorIs(t, err, assert.AnError)
		assert.Contains(t, err.Error(), "failed to restore settings")
	})

	t.Run("not when configuring failed", func(t *testing.T) {
		ws, installer, _ := newRunner(t)
		installer.EXPECT().ConfigureDatabase(anyCtx, "/project", "site1").Return(assert.AnError)

		require.ErrorIs(t, updateSite1(t.Context(), ws), assert.AnError)
	})

	t.Run("of every configured site after another failed to configure, naming each failure", func(t *testing.T) {
		ws, installer, drushMock := newRunner(t)
		ws.config.Sites = []string{"site1", "site2"}
		ws.config.Concurrency = 2
		site2Configured := make(chan struct{})
		installer.EXPECT().ConfigureDatabase(anyCtx, "/project", "site1").RunAndReturn(func(context.Context, string, string) error {
			<-site2Configured
			return assert.AnError
		})
		installer.EXPECT().ConfigureDatabase(anyCtx, "/project", "site2").RunAndReturn(func(context.Context, string, string) error {
			close(site2Configured)
			return nil
		})
		drushMock.EXPECT().Facts(anyCtx, "/project", "site2").RunAndReturn(func(ctx context.Context, _, _ string) (drush.Facts, error) {
			<-ctx.Done()
			return drush.Facts{}, ctx.Err()
		})
		installer.EXPECT().RestoreSettings(anyCtx, "/project", "site2").Return(errors.New("site2 unwritable")).Once()

		err := ws.updateSites(t.Context())
		require.ErrorIs(t, err, assert.AnError)
		assert.Contains(t, err.Error(), "failed to restore settings of site2: site2 unwritable")
	})
}

// Drupdater commits the exported configuration itself, as it does an addon's changes: Drush's own
// --commit would run git, its hooks and its identity, in the user's repository.
func TestCommitSiteChangesCommitsTheExportedConfiguration(t *testing.T) {
	newRunner := func(t *testing.T) (*run, *MockCommitter) {
		t.Helper()
		drush := NewMockDrush(t)
		committer := NewMockCommitter(t)
		drush.EXPECT().ExportConfiguration(anyCtx, "/project", "site1").Return(nil)
		r := testRun(services{logger: zap.NewNop(), drush: drush}, repo.Checkout{Path: "/project"})
		r.committer = committer
		return r, committer
	}

	t.Run("the site's sync directory, under its own message", func(t *testing.T) {
		ws, committer := newRunner(t)
		committer.EXPECT().Commit(anyCtx, "Update configuration site1", []string{"config/site1"}).Return(true, nil)

		require.NoError(t, ws.commitSiteChanges(t.Context(), "site1", syncFacts("/project/config/site1")))
	})

	t.Run("no sync directory at all", func(t *testing.T) {
		ws, _ := newRunner(t)

		err := ws.commitSiteChanges(t.Context(), "site1", syncFacts(""))
		require.ErrorContains(t, err, "site site1 has no config sync directory")
	})

	t.Run("a refused commit", func(t *testing.T) {
		ws, committer := newRunner(t)
		committer.EXPECT().Commit(anyCtx, "Update configuration site1", []string{"config/site1"}).Return(false, assert.AnError)

		err := ws.commitSiteChanges(t.Context(), "site1", syncFacts("/project/config/site1"))
		require.ErrorIs(t, err, assert.AnError)
		require.ErrorContains(t, err, "failed to commit the exported configuration")
	})
}

func TestEnsureUpdateBranchAvailable(t *testing.T) {
	const branch = "update-abc123"

	newCheckout := func(t *testing.T) *MockGitRepository {
		t.Helper()
		checkout := NewMockGitRepository(t)
		checkout.EXPECT().BranchExists(anyCtx, branch).Return(false, nil)
		return checkout
	}

	t.Run("a branch left locally by a failed run aborts", func(t *testing.T) {
		checkout := NewMockGitRepository(t)
		checkout.EXPECT().BranchExists(anyCtx, branch).Return(true, nil)
		ws := testRun(services{logger: zap.NewNop(), config: config.Config{Token: "tok"}}, repo.Checkout{Repository: checkout, Path: testCheckout})

		err := ws.ensureUpdateBranchAvailable(t.Context(), branch)
		var abort AbortError
		require.ErrorAs(t, err, &abort)
		assert.Contains(t, abort.Msg, "already exists locally")
	})

	t.Run("a failed local lookup is surfaced", func(t *testing.T) {
		checkout := NewMockGitRepository(t)
		checkout.EXPECT().BranchExists(anyCtx, branch).Return(false, assert.AnError)
		ws := testRun(services{logger: zap.NewNop()}, repo.Checkout{Repository: checkout, Path: testCheckout})

		err := ws.ensureUpdateBranchAvailable(t.Context(), branch)
		require.ErrorIs(t, err, assert.AnError)
		assert.NotErrorAs(t, err, &AbortError{})
	})

	t.Run("a dry run never reaches the remote", func(t *testing.T) {
		// RemoteBranchExists is left unstubbed, so the mock fails the test if it is called.
		ws := testRun(services{logger: zap.NewNop(), config: config.Config{DryRun: true}}, repo.Checkout{Repository: newCheckout(t), Path: testCheckout})

		require.NoError(t, ws.ensureUpdateBranchAvailable(t.Context(), branch))
	})

	t.Run("a real run asks the remote and aborts when the branch is taken", func(t *testing.T) {
		checkout := newCheckout(t)
		checkout.EXPECT().RemoteBranchExists(anyCtx, branch, "tok").Return(true, nil)
		ws := testRun(services{logger: zap.NewNop(), config: config.Config{Token: "tok"}}, repo.Checkout{Repository: checkout, Path: testCheckout})

		err := ws.ensureUpdateBranchAvailable(t.Context(), branch)
		var abort AbortError
		require.ErrorAs(t, err, &abort)
	})

	t.Run("a real run proceeds when the remote does not have the branch", func(t *testing.T) {
		checkout := newCheckout(t)
		checkout.EXPECT().RemoteBranchExists(anyCtx, branch, "tok").Return(false, nil)
		ws := testRun(services{logger: zap.NewNop(), config: config.Config{Token: "tok"}}, repo.Checkout{Repository: checkout, Path: testCheckout})

		require.NoError(t, ws.ensureUpdateBranchAvailable(t.Context(), branch))
	})

	t.Run("a remote failure is surfaced", func(t *testing.T) {
		checkout := newCheckout(t)
		checkout.EXPECT().RemoteBranchExists(anyCtx, branch, "").Return(false, assert.AnError)
		ws := testRun(services{logger: zap.NewNop()}, repo.Checkout{Repository: checkout, Path: testCheckout})

		err := ws.ensureUpdateBranchAvailable(t.Context(), branch)
		require.ErrorContains(t, err, "failed to check if branch exists")
	})
}

// Outside the temp dir, so a mutant of cleanup's guard has nothing real to delete.
const testCheckout = "/drupdater-test/checkout"

// testRun is a run over checkout with s, built by newRun as Run builds one.
func testRun(s services, checkout repo.Checkout, addons ...Addon) *run {
	rec := report.NewRecorder(buildinfo.Version, report.ModeNormal, false, "", "", nil)
	return newRun(s, checkout, NewPipeline(addons), rec)
}

// anyCtx matches any non-nil context.Context: the errgroup derives a child, so the exact value
// cannot be pinned, but non-nil still catches a call site that stops propagating it.
var anyCtx = mock.MatchedBy(func(ctx context.Context) bool { return ctx != nil })

// expectVersionLookup declares the version call every run makes. What the versions end up as is
// workflow_report_test.go's subject, not these runs'.
func expectVersionLookup(composerMock *MockComposer) {
	composerMock.EXPECT().Version(anyCtx).Return(composer.Versions{Composer: "2.10.2", PHP: "8.3.14"}, nil)
}

// testHead is where a mocked run's HEAD stands when it creates its update branch.
const testHead = "1a2b3c"

// expectUpdateBranch lets a run create its update branch at HEAD, which a run that gets that far
// does after committing the updated dependencies.
func expectUpdateBranch(repository *MockGitRepository) {
	repository.EXPECT().Head(anyCtx).Return(testHead, nil).Maybe()
	repository.EXPECT().SetBranch(anyCtx, mock.Anything, testHead).Return(nil).Maybe()
	repository.EXPECT().RemoveBranch(anyCtx, mock.Anything).Return(nil).Maybe()
}

func TestStageScaffoldChanges(t *testing.T) {
	logger := zap.NewNop()

	newComposer := func(webroot string) *MockComposer {
		c := NewMockComposer(t)
		c.EXPECT().Project(anyCtx, "/project").Return(composer.Project{WebRoot: webroot}, nil).Maybe()
		return c
	}

	t.Run("stages the changed tracked files in the web root, sorted", func(t *testing.T) {
		// A core update rewrites the scaffolded files; vendor/ is untracked and belongs to nobody.
		worktree := NewMockWorktree(t)
		worktree.EXPECT().Unstaged(anyCtx).Return([]string{"web/robots.txt", "web/.htaccess"}, nil)
		worktree.EXPECT().Stage(anyCtx, []string{"web/.htaccess", "web/robots.txt"}).Return(nil)
		core, logs := observer.New(zap.InfoLevel)

		ws := testRun(services{
			logger:   zap.New(core),
			config:   config.Config{Sites: []string{"default"}},
			composer: newComposer("web"),
		}, repo.Checkout{Worktree: worktree, Path: "/project"})

		require.NoError(t, ws.stageScaffoldChanges(context.Background()))
		require.Equal(t, 1, logs.FilterMessage("staged scaffold changes").Len())
	})

	t.Run("stages nothing outside the web root", func(t *testing.T) {
		// Regression: staging every tracked change swept the config export into the
		// shared-code commit, still carrying the installer's sqlite entry, and the next run
		// then refused to install from it. Those trees belong to the per-site export.
		// "webhooks/" is in the list because a prefix match on "web" rather than "web/" would
		// pull it in.
		worktree := NewMockWorktree(t)
		worktree.EXPECT().Unstaged(anyCtx).Return([]string{
			"config/sync/core.extension.yml", "config/second/system.site.yml",
			"translations/drupal.de.po", "web/robots.txt", "webhooks/notes.md",
		}, nil)
		worktree.EXPECT().Stage(anyCtx, []string{"web/robots.txt"}).Return(nil)

		ws := testRun(services{
			logger:   logger,
			config:   config.Config{Sites: []string{"default"}},
			composer: newComposer("web"),
		}, repo.Checkout{Worktree: worktree, Path: "/project"})

		require.NoError(t, ws.stageScaffoldChanges(context.Background()))
	})

	t.Run("stages nothing when only files outside the web root changed", func(t *testing.T) {
		// Stage is left unstubbed, so the mock fails the test if it is called.
		worktree := NewMockWorktree(t)
		worktree.EXPECT().Unstaged(anyCtx).Return([]string{"config/sync/core.extension.yml"}, nil)

		ws := testRun(services{logger: logger, composer: newComposer("web")}, repo.Checkout{Worktree: worktree, Path: "/project"})

		require.NoError(t, ws.stageScaffoldChanges(context.Background()))
	})

	t.Run("looks up no web root when nothing changed", func(t *testing.T) {
		worktree := NewMockWorktree(t)
		worktree.EXPECT().Unstaged(anyCtx).Return(nil, nil)

		ws := testRun(services{logger: logger, composer: NewMockComposer(t)}, repo.Checkout{Worktree: worktree, Path: "/project"})

		require.NoError(t, ws.stageScaffoldChanges(context.Background()))
	})

	t.Run("propagates a status failure", func(t *testing.T) {
		worktree := NewMockWorktree(t)
		worktree.EXPECT().Unstaged(anyCtx).Return(nil, errors.New("boom"))

		ws := testRun(services{logger: logger, composer: newComposer("web")}, repo.Checkout{Worktree: worktree, Path: "/project"})

		err := ws.stageScaffoldChanges(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to read worktree status")
	})

	t.Run("refuses a project without a web root", func(t *testing.T) {
		worktree := NewMockWorktree(t)
		worktree.EXPECT().Unstaged(anyCtx).Return([]string{"web/robots.txt"}, nil)

		ws := testRun(services{logger: logger, composer: newComposer("")}, repo.Checkout{Worktree: worktree, Path: "/project"})

		err := ws.stageScaffoldChanges(context.Background())
		require.EqualError(t, err, "failed to determine web root: extra.drupal-scaffold.locations.web-root is not defined")
	})

	t.Run("propagates a web root failure", func(t *testing.T) {
		worktree := NewMockWorktree(t)
		worktree.EXPECT().Unstaged(anyCtx).Return([]string{"web/robots.txt"}, nil)

		composerCLI := NewMockComposer(t)
		noWebRoot := errors.New("no web root")
		composerCLI.EXPECT().Project(anyCtx, "/project").Return(composer.Project{WebRoot: "web"}, noWebRoot)

		ws := testRun(services{logger: logger, composer: composerCLI}, repo.Checkout{Worktree: worktree, Path: "/project"})

		err := ws.stageScaffoldChanges(context.Background())
		require.ErrorIs(t, err, noWebRoot, "a failed read is not mistaken for the web root it happened to return")
		assert.Contains(t, err.Error(), "failed to determine web root")
	})

	t.Run("propagates a staging failure", func(t *testing.T) {
		worktree := NewMockWorktree(t)
		worktree.EXPECT().Unstaged(anyCtx).Return([]string{"web/robots.txt"}, nil)
		worktree.EXPECT().Stage(anyCtx, []string{"web/robots.txt"}).Return(assert.AnError)

		ws := testRun(services{logger: logger, composer: newComposer("web")}, repo.Checkout{Worktree: worktree, Path: "/project"})

		err := ws.stageScaffoldChanges(context.Background())
		require.ErrorIs(t, err, assert.AnError)
		assert.Contains(t, err.Error(), "failed to stage web/robots.txt")
	})
}

func TestRunFailsWhenScaffoldChangesCannotBeStaged(t *testing.T) {
	// Staging what the update rewrote is part of producing the commit, not a nicety: if it
	// fails, the run must fail rather than push a branch missing the scaffold changes.
	logger := zap.NewNop()
	installer := NewMockInstaller(t)
	repositoryService := NewMockGitService(t)
	vcsProvider := NewMockPlatform(t)
	repository := NewMockGitRepository(t)
	expectUpdateBranch(repository)
	mockComposer := NewMockComposer(t)
	expectVersionLookup(mockComposer)
	drush := NewMockDrush(t)

	config := config.Config{
		RepositoryURL: "https://example.com/repo.git",
		Branch:        "main",
		Token:         "token",
		Clone:         true,
		Sites:         []string{"site1"},
	}

	worktree := NewMockWorktree(t)
	worktree.EXPECT().StageGlob(anyCtx, mock.Anything).Return(nil)
	worktree.EXPECT().Unstaged(anyCtx).Return(nil, assert.AnError)

	installer.EXPECT().Install(anyCtx, testCheckout, "site1").Return(nil)

	repositoryService.EXPECT().CloneRepository(anyCtx, config.RepositoryURL, config.Branch, config.Token, repo.Identity{Name: "user", Email: "mail"}).
		Return(repo.Checkout{Repository: repository, Worktree: worktree, Path: testCheckout, Author: testAuthor}, nil)
	repositoryService.EXPECT().IsShallowClone(anyCtx, testCheckout).Return(false, nil)
	repository.EXPECT().BranchExists(anyCtx, mock.Anything).Return(false, nil).Maybe()
	vcsProvider.EXPECT().GetUser(mock.Anything).Return("user", "mail")

	mockComposer.EXPECT().CheckPlatformReqs(anyCtx, testCheckout).Return("", nil)

	mockComposer.EXPECT().CheckAgent(anyCtx, testCheckout).Return(nil)
	mockComposer.EXPECT().Install(anyCtx, testCheckout).Return(nil)
	mockComposer.EXPECT().Update(anyCtx, testCheckout, mock.Anything, mock.Anything, false, false).
		Return([]composer.PackageChange{{Package: "drupal/core", From: "9.0.0", To: "9.1.0"}}, nil)
	mockComposer.EXPECT().Project(anyCtx, testCheckout).Return(composer.Project{WebRoot: "web", LockHash: "dummy-hash"}, nil)
	repository.EXPECT().RemoteBranchExists(anyCtx, "update-dummy-hash", "token").Return(false, nil)

	workflowService := New(logger, config, drush, vcsProvider, repositoryService, installer, mockComposer)
	err := workflowService.Run(context.Background(), nil)

	require.ErrorIs(t, err, assert.AnError)
	assert.Contains(t, err.Error(), "failed to read worktree status")
}

type runKey struct{}

func TestRunHandsAddonsTheRunsContextAndCommitter(t *testing.T) {
	installer := NewMockInstaller(t)
	repositoryService := NewMockGitService(t)
	vcsProvider := NewMockPlatform(t)
	repository := NewMockGitRepository(t)
	expectUpdateBranch(repository)
	mockComposer := NewMockComposer(t)
	expectVersionLookup(mockComposer)
	drush := NewMockDrush(t)

	config := config.Config{RepositoryURL: "https://example.com/repo.git", Branch: "main", Token: "token", Clone: true, Sites: []string{"site1"}, DryRun: true}

	worktree := NewMockWorktree(t)
	worktree.EXPECT().Stage(anyCtx, []string{"config/sync"}).Return(nil).Maybe()
	worktree.EXPECT().StageGlob(anyCtx, mock.Anything).Return(nil)
	worktree.EXPECT().Unstaged(anyCtx).Return(nil, nil).Once()
	worktree.EXPECT().Commit(anyCtx, "Update composer.json and composer.lock", mock.Anything).Return(nil)
	// What an addon committing from post-code-update reaches: the run's own worktree.
	worktree.EXPECT().Stage(anyCtx, []string{"web/modules/custom/a.php"}).Return(nil)
	worktree.EXPECT().Staged(anyCtx).Return([]string{"web/modules/custom/a.php"}, nil).Once()
	worktree.EXPECT().Commit(anyCtx, "Remove deprecations", mock.Anything).Return(nil)
	worktree.EXPECT().Staged(anyCtx).Return(nil, nil).Once()

	vcsProvider.EXPECT().GetUser(mock.Anything).Return("user", "mail")
	repositoryService.EXPECT().CloneRepository(anyCtx, config.RepositoryURL, config.Branch, config.Token, repo.Identity{Name: "user", Email: "mail"}).Return(repo.Checkout{Repository: repository, Worktree: worktree, Path: testCheckout, Author: testAuthor}, nil)
	repositoryService.EXPECT().IsShallowClone(anyCtx, testCheckout).Return(false, nil)
	repository.EXPECT().BranchExists(anyCtx, mock.Anything).Return(false, nil)
	mockComposer.EXPECT().CheckPlatformReqs(anyCtx, testCheckout).Return("", nil)
	mockComposer.EXPECT().CheckAgent(anyCtx, testCheckout).Return(nil)
	mockComposer.EXPECT().Install(anyCtx, testCheckout).Return(nil)
	mockComposer.EXPECT().Update(anyCtx, testCheckout, mock.Anything, mock.Anything, false, false).
		Return([]composer.PackageChange{{Action: "Upgrade", Package: "drupal/core", From: "9.0.0", To: "9.1.0"}}, nil)
	mockComposer.EXPECT().Project(anyCtx, testCheckout).Return(composer.Project{WebRoot: "web", LockHash: "dummy-hash"}, nil)
	installer.EXPECT().Install(anyCtx, testCheckout, "site1").Return(nil)
	installer.EXPECT().ConfigureDatabase(anyCtx, testCheckout, "site1").Return(nil)
	installer.EXPECT().RestoreSettings(anyCtx, testCheckout, "site1").Return(nil)
	drush.EXPECT().Facts(anyCtx, testCheckout, "site1").Return(syncFacts(testCheckout+"/config/sync"), nil)
	drush.EXPECT().UpdateSite(anyCtx, testCheckout, "site1").Return(nil)
	drush.EXPECT().ConfigResave(anyCtx, testCheckout, "site1").Return(nil)
	drush.EXPECT().LocalFacts(anyCtx, testCheckout, "site1").Return(syncFacts(testCheckout+"/config/sync"), nil)
	drush.EXPECT().ExportConfiguration(anyCtx, testCheckout, "site1").Return(nil)

	ctx := context.WithValue(t.Context(), runKey{}, "this run")
	seen := map[string]bool{}
	carriesRun := func(name string, e interface{ Context() context.Context }) {
		seen[name] = e.Context().Value(runKey{}) == "this run"
	}
	observer := hookAddon{Hooks{
		PreComposerUpdate: propose(Change, func(e *PreComposerUpdateEvent) (UpdatePlan, error) {
			carriesRun("pre-composer-update", e)
			return UpdatePlan{}, nil
		}),
		PostComposerUpdate: On(Change, func(e *PostComposerUpdateEvent) error {
			carriesRun("post-composer-update", e)
			return nil
		}),
		PostCodeUpdate: Produce(Change, func(e *PostCodeUpdateEvent) (Result[struct{}], error) {
			carriesRun("post-code-update", e)
			return Result[struct{}]{Commits: []Commit{{"Remove deprecations", []string{"web/modules/custom/a.php"}}}}, nil
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

// updateSite1 runs the site update phase for site1 alone.
func updateSite1(ctx context.Context, r *run) error {
	r.config.Sites = []string{"site1"}
	return r.updateSites(ctx)
}
