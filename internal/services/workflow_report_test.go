package services

import (
	"context"
	"errors"
	"testing"

	"github.com/drupdater/drupdater/internal"
	"github.com/drupdater/drupdater/internal/codehosting"
	"github.com/drupdater/drupdater/internal/report"
	"github.com/drupdater/drupdater/pkg/composer"
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
	}

	h.vcsProvider.EXPECT().GetUser(mock.Anything).Return("user", "mail").Maybe()
	h.repoSvc.EXPECT().CloneRepository(h.config.RepositoryURL, h.config.Branch, h.config.Token, "user", "mail").
		Return(h.repository, h.worktree, "/tmp", nil).Maybe()
	h.repoSvc.EXPECT().IsShallowClone("/tmp").Return(false, nil).Maybe()
	h.composer.EXPECT().CheckPlatformReqs(mock.Anything, "/tmp").Return("", nil).Maybe()
	h.composer.EXPECT().Install(mock.Anything, "/tmp").Return(nil).Maybe()

	return h
}

// expectFullRun adds the expectations for a run that gets all the way through the update.
func (h *reportHarness) expectFullRun(t *testing.T) {
	t.Helper()

	h.worktree.EXPECT().Commit(mock.Anything, mock.Anything).Return(plumbing.NewHash(""), nil).Maybe()
	h.worktree.EXPECT().AddGlob(mock.Anything).Return(nil).Maybe()
	h.worktree.EXPECT().Checkout(mock.Anything).Return(nil).Maybe()

	h.installer.EXPECT().Install(mock.Anything, "/tmp", "site1").Return(nil).Maybe()
	h.installer.EXPECT().ConfigureDatabase(mock.Anything, "/tmp", "site1").Return(nil).Maybe()
	h.drush.EXPECT().UpdateSite(mock.Anything, "/tmp", "site1").Return(nil).Maybe()
	h.drush.EXPECT().ExportConfiguration(mock.Anything, "/tmp", "site1").Return(nil).Maybe()
	h.drush.EXPECT().ConfigResave(mock.Anything, "/tmp", "site1").Return(nil).Maybe()

	h.repository.EXPECT().Reference(mock.Anything, mock.Anything).Return(nil, plumbing.ErrReferenceNotFound).Maybe()
	h.repoSvc.EXPECT().BranchExists(h.repository, mock.Anything, mock.Anything).Return(false, nil).Maybe()
	h.composer.EXPECT().GetLockHash("/tmp").Return("dummy-hash", nil).Maybe()
	h.composer.EXPECT().Update(mock.Anything, "/tmp", mock.Anything, mock.Anything, false, false).
		Return([]composer.PackageChange{{Action: "Upgrade", Package: "drupal/core", From: "9.0.0", To: "9.1.0"}}, nil).Maybe()
}

func (h *reportHarness) run(t *testing.T) error {
	t.Helper()

	svc := NewWorkflowBaseService(
		zap.NewNop(), h.config, h.drush, h.vcsProvider, h.repoSvc, h.installer, h.composer,
		event.NewManager(""),
		WithReportSink(func(rep report.Report) { h.got = &rep }),
	)

	return svc.StartUpdate(context.Background(), nil)
}

func TestReportWrittenOnSuccessfulRun(t *testing.T) {
	h := newReportHarness(t, false)
	h.expectFullRun(t)
	h.repository.EXPECT().Push(mock.Anything).Return(nil)
	h.vcsProvider.EXPECT().CreateMergeRequest(mock.Anything, mock.Anything, mock.Anything, mock.Anything, "main").
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

// The credential embedded in the repository URL must not reach the report, independently of the
// log redactor applied at write time.
func TestReportRepositoryURLHasNoCredentials(t *testing.T) {
	h := newReportHarness(t, false)
	h.expectFullRun(t)
	h.repository.EXPECT().Push(mock.Anything).Return(nil)
	h.vcsProvider.EXPECT().CreateMergeRequest(mock.Anything, mock.Anything, mock.Anything, mock.Anything, "main").
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
}

// The most valuable case: a run that fails partway leaves a report naming the phase that failed.
func TestReportWrittenOnFailureNamesThePhase(t *testing.T) {
	h := newReportHarness(t, false)
	h.worktree.EXPECT().Checkout(mock.Anything).Return(nil).Maybe()
	h.installer.EXPECT().Install(mock.Anything, "/tmp", "site1").Return(errors.New("drush exploded"))
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
	h.installer.EXPECT().Install(mock.Anything, "/tmp", "site1").Return(nil).Maybe()
	h.installer.EXPECT().ConfigureDatabase(mock.Anything, "/tmp", "site1").Return(nil).Maybe()
	h.repository.EXPECT().Head().Return(nil, plumbing.ErrReferenceNotFound).Maybe()
	h.composer.EXPECT().Update(mock.Anything, "/tmp", mock.Anything, mock.Anything, false, false).
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

func phaseNames(phases []report.Phase) []string {
	names := make([]string, 0, len(phases))
	for _, p := range phases {
		names = append(names, p.Name)
	}

	return names
}
