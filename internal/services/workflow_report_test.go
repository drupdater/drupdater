package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/drupdater/drupdater/internal"
	"github.com/drupdater/drupdater/internal/codehosting"
	"github.com/drupdater/drupdater/internal/report"
	"github.com/drupdater/drupdater/pkg/composer"
	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/gookit/event"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// reportHarness wires up the mocks a StartUpdate run needs, with a report sink attached. Each
// test then overrides only the expectations relevant to the outcome it exercises.
type reportHarness struct {
	config      internal.Config
	installer   *MockInstaller
	repoSvc     *MockRepository
	vcsProvider *MockPlatform
	repository  *MockGitRepository
	composer    *MockComposer
	drush       *MockDrush
	worktree    *MockWorktree

	// addons are handed to StartUpdate and registered with the run's event manager, the same
	// way cmd/root.go wires the real ones up. Empty for the runs that do not care.
	addons []internal.Addon

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
		config: internal.Config{
			RepositoryURL: repoURL,
			Branch:        "main",
			Token:         testToken,
			Clone:         true,
			Sites:         []string{"site1"},
			DryRun:        dryRun,
		},
		installer:   NewMockInstaller(t),
		repoSvc:     NewMockRepository(t),
		vcsProvider: NewMockPlatform(t),
		repository:  NewMockGitRepository(t),
		composer:    NewMockComposer(t),
		drush:       NewMockDrush(t),
		worktree:    NewMockWorktree(t),

		versions: composer.Versions{Composer: "2.10.2", PHP: "8.3.14"},
	}

	h.composer.EXPECT().Version(anyCtx).
		RunAndReturn(func(context.Context) (composer.Versions, error) { return h.versions, h.versionsErr }).Maybe()
	h.vcsProvider.EXPECT().GetUser(mock.Anything).Return("user", "mail").Maybe()
	h.repoSvc.EXPECT().CloneRepository(h.config.RepositoryURL, h.config.Branch, h.config.Token, "user", "mail").
		Return(h.repository, h.worktree, "/tmp", nil).Maybe()
	h.repoSvc.EXPECT().IsShallowClone("/tmp").Return(false, nil).Maybe()
	h.composer.EXPECT().CheckPlatformReqs(anyCtx, "/tmp").Return("", nil).Maybe()
	h.composer.EXPECT().Install(anyCtx, "/tmp").Return(nil).Maybe()

	return h
}

// expectFullRun adds the expectations for a run that gets all the way through the update.
func (h *reportHarness) expectFullRun(t *testing.T) {
	t.Helper()

	h.worktree.EXPECT().Commit(mock.Anything, mock.Anything).Return(plumbing.NewHash(""), nil).Maybe()
	h.worktree.EXPECT().AddGlob(mock.Anything).Return(nil).Maybe()
	h.worktree.EXPECT().Status().Return(git.Status{}, nil).Maybe()
	h.worktree.EXPECT().Checkout(mock.Anything).Return(nil).Maybe()

	h.installer.EXPECT().Install(anyCtx, "/tmp", "site1").Return(nil).Maybe()
	h.installer.EXPECT().ConfigureDatabase(anyCtx, "/tmp", "site1").Return(nil).Maybe()
	h.drush.EXPECT().UpdateSite(anyCtx, "/tmp", "site1").Return(nil).Maybe()
	h.drush.EXPECT().ExportConfiguration(anyCtx, "/tmp", "site1").Return(nil).Maybe()
	h.drush.EXPECT().ConfigResave(anyCtx, "/tmp", "site1").Return(nil).Maybe()

	h.repository.EXPECT().Reference(mock.Anything, mock.Anything).Return(nil, plumbing.ErrReferenceNotFound).Maybe()
	h.repoSvc.EXPECT().BranchExists(h.repository, mock.Anything, mock.Anything).Return(false, nil).Maybe()
	h.composer.EXPECT().GetLockHash("/tmp").Return("dummy-hash", nil).Maybe()
	h.composer.EXPECT().Update(anyCtx, "/tmp", mock.Anything, mock.Anything, false, false).
		Return([]composer.PackageChange{{Action: "Upgrade", Package: "drupal/core", From: "9.0.0", To: "9.1.0"}}, nil).Maybe()
}

// withAddons registers addons with the run, so a test can stand in for the real ones where the
// workflow reads back what an addon contributed.
func (h *reportHarness) withAddons(addons ...internal.Addon) {
	h.addons = addons
}

func (h *reportHarness) run(t *testing.T) error {
	t.Helper()

	dispatcher := event.NewManager("")
	for _, addon := range h.addons {
		dispatcher.AddSubscriber(addon)
	}

	svc := NewWorkflowBaseService(
		zap.NewNop(), h.config, h.drush, h.vcsProvider, h.repoSvc, h.installer, h.composer,
		dispatcher,
		WithReportSink(func(rep report.Report) { h.got = &rep }),
	)

	return svc.StartUpdate(context.Background(), h.addons)
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
	assert.Equal(t, internal.Version, h.got.DrupdaterVersion)
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

// A --dry-run stops before publishing but is still a successful run: it did everything it was
// asked to do. This is one of the cases that produces no merge request and therefore no output
// at all without --report.
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
	h.worktree.EXPECT().Checkout(mock.Anything).Return(nil).Maybe()
	h.installer.EXPECT().Install(anyCtx, "/tmp", "site1").Return(errors.New("drush exploded"))
	h.repository.EXPECT().Head().Return(nil, plumbing.ErrReferenceNotFound).Maybe()

	err := h.run(t)

	require.Error(t, err)
	require.NotNil(t, h.got, "a failing run must still produce a report")
	assert.Equal(t, report.StatusFailed, h.got.Status)
	assert.Equal(t, "baseline site install", h.got.FailedPhase)
	assert.Contains(t, h.got.Error, "drush exploded")
	assert.Nil(t, h.got.MergeRequest)
}

// Acquiring the working copy is the first thing that can fail, and one of the most common ways
// a run fails in practice: a bad token, an unreachable host, an unreadable checkout. The report
// must still be written — this is precisely the case "written on every exit path" is for.
func TestReportWrittenWhenAcquiringTheWorkingCopyFails(t *testing.T) {
	h := newReportHarness(t, false)
	h.repoSvc.ExpectedCalls = nil
	h.repoSvc.EXPECT().CloneRepository(mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(nil, nil, "", errors.New("authentication failed"))

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
	h.worktree.EXPECT().Checkout(mock.Anything).Return(nil).Maybe()
	h.installer.EXPECT().Install(anyCtx, "/tmp", "site1").Return(nil).Maybe()
	h.installer.EXPECT().ConfigureDatabase(anyCtx, "/tmp", "site1").Return(nil).Maybe()
	h.repository.EXPECT().Head().Return(nil, plumbing.ErrReferenceNotFound).Maybe()
	h.composer.EXPECT().Update(anyCtx, "/tmp", mock.Anything, mock.Anything, false, false).
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

	svc := NewWorkflowBaseService(
		zap.NewNop(), h.config, h.drush, h.vcsProvider, h.repoSvc, h.installer, h.composer,
		event.NewManager(""),
	)

	require.NoError(t, svc.StartUpdate(context.Background(), nil))
	assert.Nil(t, h.got)
}

// mergeRequestAddon stands in for a real addon at the two points the merge request is assembled:
// it renames the title through pre-merge-request-create, the way composer_audit re-labels a
// security run, and contributes a section to the description.
type mergeRequestAddon struct {
	title   string
	section string
	// err makes the pre-merge-request-create handler fail, standing in for an addon that cannot
	// produce its part of the merge request. renderErr does the same one step later, when the
	// description template asks the addon for its section.
	err       error
	renderErr error
}

func (a *mergeRequestAddon) SubscribedEvents() map[string]any {
	return map[string]any{
		"pre-merge-request-create": event.ListenerItem{
			Priority: event.Normal,
			Listener: event.ListenerFunc(func(e event.Event) error {
				if a.err != nil {
					return a.err
				}
				e.(*PreMergeRequestCreateEvent).Title = a.title

				return nil
			}),
		},
	}
}

func (a *mergeRequestAddon) RenderTemplate() (string, error) { return a.section, a.renderErr }

var _ internal.Addon = (*mergeRequestAddon)(nil)

// A --dry-run opens no merge request, but it does assemble one. Recording the title and
// description is what lets a dry run be reviewed at all -- and what makes a broken description
// template visible before a real run has pushed anything.
func TestReportRecordsMergeRequestContentOnDryRun(t *testing.T) {
	h := newReportHarness(t, true)
	h.expectFullRun(t)
	h.withAddons(&mergeRequestAddon{
		title:   "2026-07-25: Drupal Security Updates",
		section: "## Security Report\n",
	})

	require.NoError(t, h.run(t))

	require.NotNil(t, h.got)
	assert.Equal(t, "2026-07-25: Drupal Security Updates", h.got.MergeRequestTitle,
		"the title an addon set must be the one the dry run reports")
	assert.Contains(t, h.got.MergeRequestDescription, "## Security Report")
	assert.Contains(t, phaseNames(h.got.Phases), "render merge request")
	assert.NotContains(t, phaseNames(h.got.Phases), "publish")
	assert.Nil(t, h.got.MergeRequest)
}

// The reported content has to be the published content, not a second rendering of it: a report
// that showed something other than what the reviewer sees on the merge request would be worse
// than no report at all.
func TestReportedMergeRequestContentIsWhatWasPublished(t *testing.T) {
	h := newReportHarness(t, false)
	h.expectFullRun(t)
	h.withAddons(&mergeRequestAddon{title: "custom title", section: "## Section\n"})
	h.repository.EXPECT().Push(mock.Anything).Return(nil)

	var publishedTitle, publishedDescription string
	h.vcsProvider.EXPECT().CreateMergeRequest(anyCtx, mock.Anything, mock.Anything, mock.Anything, "main").
		RunAndReturn(func(_ context.Context, title, description, _, _ string) (codehosting.MergeRequest, error) {
			publishedTitle, publishedDescription = title, description

			return codehosting.MergeRequest{URL: "https://example.com/mr/1"}, nil
		})

	require.NoError(t, h.run(t))

	require.NotNil(t, h.got)
	assert.Equal(t, "custom title", publishedTitle)
	assert.Contains(t, publishedDescription, "## Section")
	assert.Equal(t, publishedTitle, h.got.MergeRequestTitle)
	assert.Equal(t, publishedDescription, h.got.MergeRequestDescription)
}

// Assembling the merge request is a phase like any other, so a failure there is attributed
// rather than surfacing as an unexplained error at the end of a run. Both halves can fail: the
// event that settles the title, and the template that renders the description.
func TestReportNamesTheRenderPhaseWhenAssemblyFails(t *testing.T) {
	t.Run("the title event fails", func(t *testing.T) {
		titleErr := errors.New("addon could not produce a title")
		h := newReportHarness(t, true)
		h.expectFullRun(t)
		h.withAddons(&mergeRequestAddon{err: titleErr})

		err := h.run(t)

		// Wrapped, not replaced: a caller matching on its own sentinel error still can.
		require.ErrorIs(t, err, titleErr)
		require.NotNil(t, h.got)
		assert.Equal(t, report.StatusFailed, h.got.Status)
		assert.Equal(t, "render merge request", h.got.FailedPhase)
		assert.Contains(t, h.got.Error, "failed to fire event")
		assert.Contains(t, h.got.Error, "addon could not produce a title")
		assert.Empty(t, h.got.MergeRequestTitle)
		assert.Empty(t, h.got.MergeRequestDescription)
	})

	t.Run("an addon cannot render its section", func(t *testing.T) {
		renderErr := errors.New("template exploded")
		h := newReportHarness(t, true)
		h.expectFullRun(t)
		h.withAddons(&mergeRequestAddon{title: "a title", renderErr: renderErr})

		err := h.run(t)

		require.ErrorIs(t, err, renderErr)
		require.NotNil(t, h.got)
		assert.Equal(t, report.StatusFailed, h.got.Status)
		assert.Equal(t, "render merge request", h.got.FailedPhase)
		assert.Contains(t, h.got.Error, "failed to generate description")
		assert.Empty(t, h.got.MergeRequestDescription)
	})
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
