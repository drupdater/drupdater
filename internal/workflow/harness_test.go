package workflow

import (
	"context"
	"testing"

	"github.com/drupdater/drupdater/internal/codehosting"
	"github.com/drupdater/drupdater/internal/composer"
	"github.com/drupdater/drupdater/internal/config"
	"github.com/drupdater/drupdater/internal/repo"
	"github.com/drupdater/drupdater/internal/report"
	"github.com/stretchr/testify/mock"
	"go.uber.org/zap"
)

// runHarness wires up the mocks a Run needs. A test declares its own expectations for the
// outcome it exercises; the harness's defaults are registered when the run starts, after the
// test's, so the test's answer first regardless of the order they were written in.
//
// The defaults are all .Maybe(): a call the test leaves to them is allowed, not required. A
// mock the harness was not asked to cover stays strict, so an unexpected call fails the test.
type runHarness struct {
	config      config.Config
	logger      *zap.Logger
	installer   *MockInstaller
	repoSvc     *MockGitService
	vcsProvider *MockPlatform
	repository  *MockGitRepository
	composer    *MockComposer
	drush       *MockDrush
	worktree    *MockWorktree

	// path is where the working copy is checked out.
	path string
	// withoutPlatform runs with a nil Platform, as cmd does for a token-free dry run.
	withoutPlatform bool

	// addons are handed to Run, the same way cmd/root.go wires the real ones up.
	addons []Addon

	// What the version lookup returns. Fields, so a test can make it fail.
	versions    composer.Versions
	versionsErr error

	update, fullRun, publish bool

	got *report.Report
}

func newRunHarness(t *testing.T, dryRun bool) *runHarness {
	t.Helper()

	// Assembled rather than written as one literal so the fixture is not mistaken for a real
	// embedded credential by static analysis.
	const testToken = "s3cret"
	repoURL := "https://user:" + testToken + "@example.com/repo.git"

	return &runHarness{
		config: config.Config{
			RepositoryURL: repoURL,
			Branch:        "main",
			Token:         testToken,
			Clone:         true,
			Sites:         []string{"site1"},
			DryRun:        dryRun,
		},
		logger:      zap.NewNop(),
		installer:   NewMockInstaller(t),
		repoSvc:     NewMockGitService(t),
		vcsProvider: NewMockPlatform(t),
		repository:  NewMockGitRepository(t),
		composer:    NewMockComposer(t),
		drush:       NewMockDrush(t),
		worktree:    NewMockWorktree(t),
		path:        testCheckout,

		versions: composer.Versions{Composer: "2.10.2", PHP: "8.3.14"},
	}
}

// expectUpdate lets Composer resolve one upgrade, drupal/core 9.0.0 → 9.1.0.
func (h *runHarness) expectUpdate() { h.update = true }

// expectFullRun lets the run get all the way through updating every site and creating its
// update branch.
func (h *runHarness) expectFullRun() { h.update, h.fullRun = true, true }

// expectPublish lets a full run push its branch and open a merge request.
func (h *runHarness) expectPublish() { h.update, h.fullRun, h.publish = true, true, true }

func (h *runHarness) withAddons(addons ...Addon) { h.addons = addons }

func (h *runHarness) identity() repo.Identity {
	if h.withoutPlatform {
		return repo.Identity{}
	}
	return repo.Identity{Name: "user", Email: "mail"}
}

// expectDefaults lets every run get as far as resolving its update.
func (h *runHarness) expectDefaults() {
	checkout := repo.Checkout{Repository: h.repository, Worktree: h.worktree, Path: h.path, Author: testAuthor}

	h.composer.EXPECT().Version(anyCtx).
		RunAndReturn(func(context.Context) (composer.Versions, error) { return h.versions, h.versionsErr }).Maybe()
	h.vcsProvider.EXPECT().GetUser(mock.Anything).Return("user", "mail").Maybe()
	if h.config.Clone {
		h.repoSvc.EXPECT().CloneRepository(anyCtx, h.config.RepositoryURL, h.config.Branch, h.config.Token, h.identity()).
			Return(checkout, nil).Maybe()
	} else {
		h.repoSvc.EXPECT().AddWorktree(anyCtx, h.config.WorkingDir, h.identity()).Return(checkout, nil).Maybe()
		h.repoSvc.EXPECT().RemoveWorktree(anyCtx, h.config.WorkingDir, h.path).Return(nil).Maybe()
	}
	h.repoSvc.EXPECT().IsShallowClone(anyCtx, h.path).Return(false, nil).Maybe()
	h.composer.EXPECT().CheckPlatformReqs(anyCtx, h.path).Return("", nil).Maybe()
	h.composer.EXPECT().CheckAgent(anyCtx, h.path).Return(nil).Maybe()
	h.composer.EXPECT().Install(anyCtx, h.path).Return(nil).Maybe()

	if h.update {
		h.composer.EXPECT().Update(anyCtx, h.path, mock.Anything, mock.Anything, false, false).
			Return([]composer.PackageChange{{Action: "Upgrade", Package: "drupal/core", From: "9.0.0", To: "9.1.0"}}, nil).Maybe()
		h.composer.EXPECT().Project(anyCtx, h.path).Return(composer.Project{WebRoot: "web", LockHash: "dummy-hash"}, nil).Maybe()
		h.worktree.EXPECT().Committed(anyCtx, "composer.lock").Return([]byte(testBaseLock), nil).Maybe()
	}
	if h.fullRun {
		h.expectFullRunDefaults()
	}
	if h.publish {
		h.repository.EXPECT().Push(anyCtx, "update-dummy-hash", h.config.Token).Return(nil).Maybe()
		h.vcsProvider.EXPECT().CreateMergeRequest(anyCtx, mock.Anything, mock.Anything, "update-dummy-hash", h.config.Branch).
			Return(codehosting.MergeRequest{URL: "https://example.com/mr/1"}, nil).Maybe()
	}
}

func (h *runHarness) expectFullRunDefaults() {
	h.repository.EXPECT().BranchExists(anyCtx, mock.Anything).Return(false, nil).Maybe()
	// A dry run never pushes, so it must not ask the remote either: left strict there.
	if !h.config.DryRun {
		h.repository.EXPECT().RemoteBranchExists(anyCtx, mock.Anything, mock.Anything).Return(false, nil).Maybe()
	}
	expectUpdateBranch(h.repository)

	h.worktree.EXPECT().Commit(anyCtx, mock.Anything, mock.Anything).Return(nil).Maybe()
	h.worktree.EXPECT().StageGlob(anyCtx, mock.Anything).Return(nil).Maybe()
	h.worktree.EXPECT().Stage(anyCtx, []string{"config/sync"}).Return(nil).Maybe()
	h.worktree.EXPECT().Unstaged(anyCtx).Return(nil, nil).Maybe()
	h.worktree.EXPECT().Staged(anyCtx).Return(nil, nil).Maybe()

	for _, site := range h.config.Sites {
		h.installer.EXPECT().Install(anyCtx, h.path, site).Return(nil).Maybe()
		h.installer.EXPECT().ConfigureDatabase(anyCtx, h.path, site).Return(nil).Maybe()
		h.installer.EXPECT().RestoreSettings(anyCtx, h.path, site).Return(nil).Maybe()
		h.drush.EXPECT().Facts(anyCtx, h.path, site).Return(syncFacts(h.path+"/config/sync"), nil).Maybe()
		h.drush.EXPECT().UpdateSite(anyCtx, h.path, site).Return(nil).Maybe()
		h.drush.EXPECT().ConfigResave(anyCtx, h.path, site).Return(nil).Maybe()
		h.drush.EXPECT().LocalFacts(anyCtx, h.path, site).Return(syncFacts(h.path+"/config/sync"), nil).Maybe()
		h.drush.EXPECT().ExportConfiguration(anyCtx, h.path, site).Return(nil).Maybe()
	}
}

// runner is the harness's Runner. Every call builds a new one; run it more than once to reuse it.
func (h *runHarness) runner() *Runner {
	h.expectDefaults()
	var platform Platform
	if !h.withoutPlatform {
		platform = h.vcsProvider
	}
	return New(h.logger, h.config, h.drush, platform, h.repoSvc, h.installer, h.composer,
		WithReportSink(func(rep report.Report) { h.got = &rep }))
}

func (h *runHarness) run(t *testing.T) error {
	t.Helper()
	return h.runner().Run(t.Context(), h.addons)
}
