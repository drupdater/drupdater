package workflow

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/drupdater/drupdater/internal/repo"

	"github.com/drupdater/drupdater/internal/buildinfo"
	"github.com/drupdater/drupdater/internal/codehosting"
	"github.com/drupdater/drupdater/internal/composer"
	"github.com/drupdater/drupdater/internal/config"
	"github.com/drupdater/drupdater/internal/report"
	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// reportHarness wires up the mocks a Run run needs, with a report sink attached. Each
// test then overrides only the expectations relevant to the outcome it exercises.
type reportHarness struct {
	config      config.Config
	installer   *MockInstaller
	repoSvc     *MockGitService
	vcsProvider *MockPlatform
	repository  *MockGitRepository
	composer    *MockComposer
	drush       *MockDrush
	worktree    *MockWorktree

	// addons are handed to Run and registered with the run's event manager, the same
	// way cmd/root.go wires the real ones up. Empty for the runs that do not care.
	addons []Addon

	// What the version lookup returns. Fields, so a test can make it fail after the harness has
	// wired the expectation up.
	versions    composer.Versions
	versionsErr error

	got *report.Report
}

func newReportHarness(t *testing.T, dryRun bool) *reportHarness {
	t.Helper()

	// Assembled rather than written as one literal so the fixture is not mistaken for a real
	// embedded credential by static analysis.
	const testToken = "s3cret"
	repoURL := "https://user:" + testToken + "@example.com/repo.git"

	h := &reportHarness{
		config: config.Config{
			RepositoryURL: repoURL,
			Branch:        "main",
			Token:         testToken,
			Clone:         true,
			Sites:         []string{"site1"},
			DryRun:        dryRun,
		},
		installer:   NewMockInstaller(t),
		repoSvc:     NewMockGitService(t),
		vcsProvider: NewMockPlatform(t),
		repository:  NewMockGitRepository(t),
		composer:    NewMockComposer(t),
		drush:       NewMockDrush(t),
		worktree:    NewMockWorktree(t),

		versions: composer.Versions{Composer: "2.10.2", PHP: "8.3.14"},
	}
	return h
}

// expectDefaults lets every run get as far as resolving its update. Registered when the run
// starts, after the test's own expectations, so those answer first.
func (h *reportHarness) expectDefaults() {
	h.composer.EXPECT().Version(anyCtx).
		RunAndReturn(func(context.Context) (composer.Versions, error) { return h.versions, h.versionsErr }).Maybe()
	h.vcsProvider.EXPECT().GetUser(mock.Anything).Return("user", "mail").Maybe()
	h.repoSvc.EXPECT().CloneRepository(h.config.RepositoryURL, h.config.Branch, h.config.Token, repo.Identity{Name: "user", Email: "mail"}).
		Return(repo.Checkout{Repository: h.repository, Worktree: h.worktree, Path: testCheckout, Author: testAuthor}, nil).Maybe()
	h.repoSvc.EXPECT().IsShallowClone(testCheckout).Return(false, nil).Maybe()
	h.composer.EXPECT().CheckPlatformReqs(anyCtx, testCheckout).Return("", nil).Maybe()
	h.composer.EXPECT().Install(anyCtx, testCheckout).Return(nil).Maybe()
}

// expectFullRun adds the expectations for a run that gets all the way through the update.
func (h *reportHarness) expectFullRun(t *testing.T) {
	t.Helper()

	h.worktree.EXPECT().Commit(mock.Anything, mock.Anything).Return(plumbing.NewHash(""), nil).Maybe()
	h.worktree.EXPECT().AddGlob(mock.Anything).Return(nil).Maybe()
	h.worktree.EXPECT().Status().Return(git.Status{}, nil).Maybe()

	h.installer.EXPECT().Install(anyCtx, testCheckout, "site1").Return(nil).Maybe()
	h.installer.EXPECT().ConfigureDatabase(anyCtx, testCheckout, "site1").Return(nil).Maybe()
	h.installer.EXPECT().RestoreSettings(anyCtx, testCheckout, "site1").Return(nil).Maybe()
	h.drush.EXPECT().UpdateSite(anyCtx, testCheckout, "site1").Return(nil).Maybe()
	h.drush.EXPECT().ExportConfiguration(anyCtx, testCheckout, "site1").Return(nil).Maybe()
	h.drush.EXPECT().Facts(anyCtx, testCheckout, "site1").Return(syncFacts(testCheckout+"/config/sync"), nil).Maybe()
	h.worktree.EXPECT().Add("config/sync").Return(plumbing.ZeroHash, nil).Maybe()
	expectUpdateBranch(h.repository)
	h.drush.EXPECT().ConfigResave(anyCtx, testCheckout, "site1").Return(nil).Maybe()
	h.drush.EXPECT().LocalFacts(anyCtx, testCheckout, "site1").Return(syncFacts(testCheckout+"/config/sync"), nil).Maybe()

	h.repository.EXPECT().Reference(mock.Anything, mock.Anything).Return(nil, plumbing.ErrReferenceNotFound).Maybe()
	h.repoSvc.EXPECT().BranchExists(h.repository, mock.Anything, mock.Anything).Return(false, nil).Maybe()
	h.composer.EXPECT().GetLockHash(testCheckout).Return("dummy-hash", nil).Maybe()
	h.composer.EXPECT().Update(anyCtx, testCheckout, mock.Anything, mock.Anything, false, false).
		Return([]composer.PackageChange{{Action: "Upgrade", Package: "drupal/core", From: "9.0.0", To: "9.1.0"}}, nil).Maybe()
}

// withAddons registers addons with the run, so a test can stand in for the real ones where the
// workflow reads back what an addon contributed.
func (h *reportHarness) withAddons(addons ...Addon) {
	h.addons = addons
}

// runner is the harness's Runner. Every call builds a new one; run it more than once to reuse it.
func (h *reportHarness) runner() *Runner {
	h.expectDefaults()
	return New(
		zap.NewNop(), h.config, h.drush, h.vcsProvider, h.repoSvc, h.installer, h.composer,
		WithReportSink(func(rep report.Report) { h.got = &rep }),
	)
}

func (h *reportHarness) run(t *testing.T) error {
	t.Helper()
	return h.runner().Run(context.Background(), h.addons)
}

func TestReportWrittenOnSuccessfulRun(t *testing.T) {
	h := newReportHarness(t, false)
	h.expectFullRun(t)
	h.repository.EXPECT().Push(mock.Anything).Return(nil)
	h.vcsProvider.EXPECT().CreateMergeRequest(anyCtx, mock.Anything, mock.Anything, mock.Anything, "main").
		Return(codehosting.MergeRequest{URL: "https://example.com/mr/1"}, nil)

	require.NoError(t, h.run(t))

	require.NotNil(t, h.got)
	assert.Equal(t, report.StatusSuccess, h.got.Status)
	assert.Empty(t, h.got.FailedPhase)
	require.NotNil(t, h.got.MergeRequest)
	assert.Equal(t, "https://example.com/mr/1", h.got.MergeRequest.URL)
	assert.NotEmpty(t, h.got.UpdateBranch)

	require.Len(t, h.got.Packages, 1)
	assert.Equal(t, "drupal/core", h.got.Packages[0].Package)
	assert.Equal(t, "9.1.0", h.got.Packages[0].To)

	// The publish phase only exists on a run that actually publishes.
	assert.Contains(t, phaseNames(h.got.Phases), "publish")
	assert.Equal(t, report.ModeNormal, h.got.Mode)
	assert.Equal(t, buildinfo.Version, h.got.DrupdaterVersion)
}

// Composer decides what a run does, so the report has to name the version that produced it.
func TestReportRecordsTheToolVersions(t *testing.T) {
	h := newReportHarness(t, false)
	h.expectFullRun(t)
	h.repository.EXPECT().Push(mock.Anything).Return(nil)
	h.vcsProvider.EXPECT().CreateMergeRequest(anyCtx, mock.Anything, mock.Anything, mock.Anything, "main").
		Return(codehosting.MergeRequest{}, nil)

	require.NoError(t, h.run(t))

	require.NotNil(t, h.got)
	assert.Equal(t, "2.10.2", h.got.ComposerVersion)
	assert.Equal(t, "8.3.14", h.got.PHPVersion)
}

// One subprocess for a field no update depends on must never be what stops a run.
func TestUnreadableToolVersionsDoNotFailTheRun(t *testing.T) {
	h := newReportHarness(t, false)
	h.versionsErr = errors.New("composer: command not found")
	h.expectFullRun(t)
	h.repository.EXPECT().Push(mock.Anything).Return(nil)
	h.vcsProvider.EXPECT().CreateMergeRequest(anyCtx, mock.Anything, mock.Anything, mock.Anything, "main").
		Return(codehosting.MergeRequest{}, nil)

	require.NoError(t, h.run(t))

	require.NotNil(t, h.got)
	assert.Equal(t, report.StatusSuccess, h.got.Status)
	assert.Empty(t, h.got.ComposerVersion)
	assert.Empty(t, h.got.PHPVersion)
}

// The credential embedded in the repository URL must not reach the report, independently of the
// log redactor applied at write time.
func TestReportRepositoryURLHasNoCredentials(t *testing.T) {
	h := newReportHarness(t, false)
	h.expectFullRun(t)
	h.repository.EXPECT().Push(mock.Anything).Return(nil)
	h.vcsProvider.EXPECT().CreateMergeRequest(anyCtx, mock.Anything, mock.Anything, mock.Anything, "main").
		Return(codehosting.MergeRequest{}, nil)

	require.NoError(t, h.run(t))

	require.NotNil(t, h.got)
	assert.Equal(t, "https://example.com/repo.git", h.got.Repository)
	assert.NotContains(t, h.got.Repository, "s3cret")
}

// A --dry-run did everything it was asked to, so it is a success — and without --report it
// produces no output at all.
func TestReportWrittenOnDryRun(t *testing.T) {
	h := newReportHarness(t, true)
	h.expectFullRun(t)

	require.NoError(t, h.run(t))

	require.NotNil(t, h.got)
	assert.Equal(t, report.StatusSuccess, h.got.Status)
	assert.True(t, h.got.DryRun)
	assert.Nil(t, h.got.MergeRequest)
	assert.NotContains(t, phaseNames(h.got.Phases), "publish")
	// With no addon renaming it, the title is the maintenance-update default, dated by month.
	assert.Contains(t, h.got.MergeRequestTitle, ": Drupal Maintenance Updates")
	assert.Contains(t, h.got.MergeRequestTitle, time.Now().Format("January 2006"))
}

// The most valuable case: a run that fails partway leaves a report naming the phase that failed.
func TestReportWrittenOnFailureNamesThePhase(t *testing.T) {
	h := newReportHarness(t, false)
	h.installer.EXPECT().Install(anyCtx, testCheckout, "site1").Return(errors.New("drush exploded"))
	h.repository.EXPECT().Head().Return(nil, plumbing.ErrReferenceNotFound).Maybe()
	h.composer.EXPECT().Update(anyCtx, testCheckout, mock.Anything, mock.Anything, false, false).
		Return([]composer.PackageChange{{Action: "Upgrade", Package: "drupal/core", From: "9.0.0", To: "9.1.0"}}, nil)
	h.composer.EXPECT().GetLockHash(testCheckout).Return("dummy-hash", nil)
	h.repository.EXPECT().Reference(mock.Anything, mock.Anything).Return(nil, plumbing.ErrReferenceNotFound)
	h.repoSvc.EXPECT().BranchExists(h.repository, "update-dummy-hash", mock.Anything).Return(false, nil)

	err := h.run(t)

	require.Error(t, err)
	require.NotNil(t, h.got, "a failing run must still produce a report")
	assert.Equal(t, report.StatusFailed, h.got.Status)
	assert.Equal(t, "baseline site install", h.got.FailedPhase)
	assert.Contains(t, h.got.Error, "drush exploded")
	assert.Nil(t, h.got.MergeRequest)
	assert.Len(t, h.got.Packages, 1, "the update is resolved before any site is installed")
}

// The merge request is rendered after the sites update, so a run failing earlier gets its addon
// sections only from the deferred write.
func TestReportOfARunFailingBeforeRenderingCarriesTheAddonSections(t *testing.T) {
	h := newReportHarness(t, false)
	h.installer.EXPECT().Install(anyCtx, testCheckout, "site1").Return(assert.AnError)
	h.repository.EXPECT().Head().Return(nil, plumbing.ErrReferenceNotFound).Maybe()
	h.composer.EXPECT().Update(anyCtx, testCheckout, mock.Anything, mock.Anything, false, false).
		Return([]composer.PackageChange{{Action: "Upgrade", Package: "drupal/core", From: "9.0.0", To: "9.1.0"}}, nil)
	h.composer.EXPECT().GetLockHash(testCheckout).Return("dummy-hash", nil)
	h.repository.EXPECT().Reference(mock.Anything, mock.Anything).Return(nil, plumbing.ErrReferenceNotFound)
	h.repoSvc.EXPECT().BranchExists(h.repository, "update-dummy-hash", mock.Anything).Return(false, nil)
	h.withAddons(&reportingAddon{key: "composer_patches", data: "conflicts"})

	require.ErrorIs(t, h.run(t), assert.AnError)

	require.NotNil(t, h.got)
	assert.Equal(t, map[string]any{"composer_patches": "conflicts"}, h.got.Addons)
}

// Acquiring the working copy is the first thing that can fail, and the case that "written on
// every exit path" exists for.
func TestReportWrittenWhenAcquiringTheWorkingCopyFails(t *testing.T) {
	h := newReportHarness(t, false)
	h.repoSvc.ExpectedCalls = nil
	h.repoSvc.EXPECT().CloneRepository(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(repo.Checkout{}, errors.New("authentication failed"))

	err := h.run(t)

	require.Error(t, err)
	require.NotNil(t, h.got, "a run that never got a working copy must still produce a report")
	assert.Equal(t, report.StatusFailed, h.got.Status)
	assert.Equal(t, "acquire working copy", h.got.FailedPhase)
	assert.Contains(t, h.got.Error, "authentication failed")
	assert.Empty(t, h.got.Packages)
	assert.Nil(t, h.got.MergeRequest)
}

// "Nothing to update" is reported as its own status rather than as a failure, so a consumer
// watching many repositories does not treat a healthy, up-to-date site as needing attention.
func TestReportNoChangesIsNotAFailure(t *testing.T) {
	h := newReportHarness(t, false)
	h.installer.EXPECT().Install(anyCtx, testCheckout, "site1").Return(nil).Maybe()
	h.installer.EXPECT().ConfigureDatabase(anyCtx, testCheckout, "site1").Return(nil).Maybe()
	h.installer.EXPECT().RestoreSettings(anyCtx, testCheckout, "site1").Return(nil).Maybe()
	h.repository.EXPECT().Head().Return(nil, plumbing.ErrReferenceNotFound).Maybe()
	h.composer.EXPECT().Update(anyCtx, testCheckout, mock.Anything, mock.Anything, false, false).
		Return([]composer.PackageChange{}, nil)

	err := h.run(t)

	require.Error(t, err, "the workflow still aborts")
	require.NotNil(t, h.got)
	assert.Equal(t, report.StatusNoChanges, h.got.Status)
	assert.Empty(t, h.got.FailedPhase)
	assert.Empty(t, h.got.Error)
}

// Without --report no sink is attached, and the run must behave exactly as before.
func TestRunWithoutReportSinkIsUnaffected(t *testing.T) {
	h := newReportHarness(t, true)
	h.expectFullRun(t)
	h.expectDefaults()

	svc := New(
		zap.NewNop(), h.config, h.drush, h.vcsProvider, h.repoSvc, h.installer, h.composer,
	)

	require.NoError(t, svc.Run(context.Background(), nil))
	assert.Nil(t, h.got)
}

// quietAddon hooks nothing and reports nothing.
type quietAddon struct{}

func (quietAddon) Hooks() Hooks { return Hooks{} }

// advisoryAddon reports one remaining advisory, which the description renders as its own section.
func advisoryAddon() *reportingAddon {
	return &reportingAddon{key: "composer_audit", data: map[string]any{
		"remaining": []map[string]string{{"cve": "CVE-2026-1", "title": "XSS", "severity": "high", "packageName": "drupal/core"}},
	}}
}

// A --dry-run assembles a merge request without opening one, which is what makes a broken
// template visible before a real run has pushed anything.
func TestReportRecordsMergeRequestContentOnDryRun(t *testing.T) {
	h := newReportHarness(t, true)
	h.expectFullRun(t)
	h.withAddons(advisoryAddon())

	require.NoError(t, h.run(t))

	require.NotNil(t, h.got)
	assert.Contains(t, h.got.MergeRequestTitle, ": Drupal Maintenance Updates")
	assert.Contains(t, h.got.MergeRequestDescription, "## 🛡️ Security Report")
	assert.Contains(t, h.got.MergeRequestDescription, "CVE-2026-1")
	assert.Contains(t, phaseNames(h.got.Phases), "render merge request")
	assert.NotContains(t, phaseNames(h.got.Phases), "publish")
	assert.Nil(t, h.got.MergeRequest)
}

// The reported content must be the published content, not a second rendering of it.
func TestReportedMergeRequestContentIsWhatWasPublished(t *testing.T) {
	h := newReportHarness(t, false)
	h.expectFullRun(t)
	h.withAddons(advisoryAddon())
	h.repository.EXPECT().Push(mock.Anything).Return(nil)

	var publishedTitle, publishedDescription string
	h.vcsProvider.EXPECT().CreateMergeRequest(anyCtx, mock.Anything, mock.Anything, mock.Anything, "main").
		RunAndReturn(func(_ context.Context, title, description, _, _ string) (codehosting.MergeRequest, error) {
			publishedTitle, publishedDescription = title, description

			return codehosting.MergeRequest{URL: "https://example.com/mr/1"}, nil
		})

	require.NoError(t, h.run(t))

	require.NotNil(t, h.got)
	assert.Contains(t, publishedDescription, "CVE-2026-1")
	assert.Equal(t, publishedTitle, h.got.MergeRequestTitle)
	assert.Equal(t, publishedDescription, h.got.MergeRequestDescription)
}

// A phase like any other, so its failure is attributed rather than unexplained: here a section
// whose shape the description cannot read.
func TestReportNamesTheRenderPhaseWhenAssemblyFails(t *testing.T) {
	h := newReportHarness(t, true)
	h.expectFullRun(t)
	h.withAddons(&reportingAddon{key: "composer_audit", data: "not an object"})

	err := h.run(t)

	require.ErrorContains(t, err, "failed to generate description")
	require.NotNil(t, h.got)
	assert.Equal(t, report.StatusFailed, h.got.Status)
	assert.Equal(t, "render merge request", h.got.FailedPhase)
	assert.Contains(t, h.got.Error, "failed to read the report's addon sections")
	// A failure after the branch was created removes it, or the next run would stop on it.
	h.repository.AssertCalled(t, "SetBranch", "update-dummy-hash", testHead.Hash())
	h.repository.AssertCalled(t, "RemoveBranch", "update-dummy-hash")
	assert.Empty(t, h.got.MergeRequestTitle)
	assert.Empty(t, h.got.MergeRequestDescription)
}

func phaseNames(phases []report.Phase) []string {
	names := make([]string, 0, len(phases))
	for _, p := range phases {
		names = append(names, p.Name)
	}

	return names
}

// Auto-merge is best-effort: a failure only warns, so the report is the one machine-readable
// place a consumer can learn that the MR it is waiting on will not merge itself.
func TestReportRecordsAutoMergeOutcome(t *testing.T) {
	t.Run("not requested leaves auto_merge absent", func(t *testing.T) {
		h := newReportHarness(t, false)
		h.expectFullRun(t)
		h.repository.EXPECT().Push(mock.Anything).Return(nil)
		h.vcsProvider.EXPECT().CreateMergeRequest(anyCtx, mock.Anything, mock.Anything, mock.Anything, "main").
			Return(codehosting.MergeRequest{URL: "https://example.com/mr/1"}, nil)

		require.NoError(t, h.run(t))

		require.NotNil(t, h.got.MergeRequest)
		assert.Nil(t, h.got.MergeRequest.AutoMerge, "absent means never asked for")
	})

	t.Run("success is recorded as enabled", func(t *testing.T) {
		h := newReportHarness(t, false)
		h.config.RunTypes.Normal.AutoMerge = true
		h.expectFullRun(t)
		h.repository.EXPECT().Push(mock.Anything).Return(nil)
		h.vcsProvider.EXPECT().CreateMergeRequest(anyCtx, mock.Anything, mock.Anything, mock.Anything, "main").
			Return(codehosting.MergeRequest{URL: "https://example.com/mr/1"}, nil)
		h.vcsProvider.EXPECT().EnableAutoMerge(anyCtx, mock.Anything).Return(nil)

		require.NoError(t, h.run(t))

		require.NotNil(t, h.got.MergeRequest.AutoMerge)
		assert.True(t, h.got.MergeRequest.AutoMerge.Enabled)
		assert.Empty(t, h.got.MergeRequest.AutoMerge.Error)
	})

	t.Run("failure is recorded without failing the run", func(t *testing.T) {
		h := newReportHarness(t, false)
		h.config.RunTypes.Normal.AutoMerge = true
		h.expectFullRun(t)
		h.repository.EXPECT().Push(mock.Anything).Return(nil)
		h.vcsProvider.EXPECT().CreateMergeRequest(anyCtx, mock.Anything, mock.Anything, mock.Anything, "main").
			Return(codehosting.MergeRequest{URL: "https://example.com/mr/1"}, nil)
		h.vcsProvider.EXPECT().EnableAutoMerge(anyCtx, mock.Anything).
			Return(errors.New("auto-merge is not allowed for this repository"))

		require.NoError(t, h.run(t), "a failed auto-merge must not fail the run")

		assert.Equal(t, report.StatusSuccess, h.got.Status)
		require.NotNil(t, h.got.MergeRequest.AutoMerge)
		assert.False(t, h.got.MergeRequest.AutoMerge.Enabled)
		assert.Contains(t, h.got.MergeRequest.AutoMerge.Error, "not allowed")
	})
}

type reportingAddon struct {
	quietAddon
	key  string
	data any
}

func (a *reportingAddon) ReportKey() string { return a.key }
func (a *reportingAddon) ReportData() any   { return a.data }

func TestReportCollectsSectionsOnlyFromReporters(t *testing.T) {
	h := newReportHarness(t, true)
	h.expectFullRun(t)
	h.withAddons(
		&reportingAddon{key: "translations_updater", data: map[string]int{"fixed": 2}},
		&reportingAddon{key: "quiet_addon"},
		quietAddon{},
	)

	require.NoError(t, h.run(t))

	require.NotNil(t, h.got)
	assert.Equal(t, map[string]any{"translations_updater": map[string]int{"fixed": 2}}, h.got.Addons)
}
