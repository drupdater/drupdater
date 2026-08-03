package services

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/drupdater/drupdater/internal"
	"github.com/drupdater/drupdater/internal/codehosting"
	"github.com/drupdater/drupdater/pkg/composer"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/gookit/event"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// occupancyProbe counts how many goroutines are inside a mocked call at once, so a phase can
// assert it fans out and another that it does not.
type occupancyProbe struct {
	current, high atomic.Int32
}

func newOccupancyProbe() *occupancyProbe { return &occupancyProbe{} }

// probe matches the (ctx, path, site) shape both mocked calls share.
func (o *occupancyProbe) probe(context.Context, string, string) error {
	n := o.current.Add(1)
	defer o.current.Add(-1)
	for {
		seen := o.high.Load()
		if n <= seen || o.high.CompareAndSwap(seen, n) {
			break
		}
	}
	// Long enough that overlapping callers are still inside when the next one arrives; without
	// it a fan-out could finish one at a time and read as serialised.
	time.Sleep(5 * time.Millisecond)
	return nil
}

func (o *occupancyProbe) peak() int32 { return o.high.Load() }

// Every other StartUpdate test runs a single site, which walks the per-site phases on one
// goroutine and leaves forEachSite's fan-out and siteCommitMu untested against each other. This
// one drives the real workflow with several sites, and asserts both halves of the design: the
// independent phase overlaps, the committing one does not.
func TestStartUpdateWithConcurrentSites(t *testing.T) {
	installer := NewMockInstaller(t)
	repositoryService := NewMockRepository(t)
	vcsProvider := NewMockPlatform(t)
	repository := NewMockGitRepository(t)
	mockComposer := NewMockComposer(t)
	expectVersionLookup(mockComposer)
	drush := NewMockDrush(t)

	sites := []string{"site1", "site2", "site3", "site4"}
	config := internal.Config{
		RepositoryURL: "https://example.com/repo.git",
		Branch:        "main",
		Token:         "token",
		Clone:         true,
		Sites:         sites,
		// Unbounded, so the sites genuinely overlap rather than running one at a time.
		Concurrency: len(sites),
	}

	worktree := NewMockWorktree(t)
	worktree.EXPECT().Commit(mock.Anything, mock.Anything).Return(plumbing.NewHash(""), nil)
	worktree.EXPECT().AddGlob(mock.Anything).Return(nil)
	worktree.EXPECT().Status().Return(git.Status{}, nil).Maybe()
	worktree.EXPECT().Checkout(workBranchCheckout).Return(nil)

	// Two opposite invariants, measured by peak occupancy rather than by the race detector: the
	// git index the commit tail writes is mocked here, so nothing about it is shared memory.
	baseline := newOccupancyProbe()
	commitTail := newOccupancyProbe()

	for _, site := range sites {
		installer.EXPECT().Install(anyCtx, "/tmp", site).RunAndReturn(baseline.probe)
		installer.EXPECT().ConfigureDatabase(anyCtx, "/tmp", site).Return(nil)
		drush.EXPECT().UpdateSite(anyCtx, "/tmp", site).Return(nil)
		drush.EXPECT().ExportConfiguration(anyCtx, "/tmp", site).RunAndReturn(commitTail.probe)
		drush.EXPECT().ConfigResave(anyCtx, "/tmp", site).Return(nil)
	}

	repositoryService.EXPECT().CloneRepository(config.RepositoryURL, config.Branch, config.Token, "user", "mail").Return(repository, worktree, "/tmp", nil)
	repositoryService.EXPECT().IsShallowClone("/tmp").Return(false, nil)
	repositoryService.EXPECT().BranchExists(repository, mock.Anything, mock.Anything).Return(false, nil)
	repository.EXPECT().Reference(mock.Anything, mock.Anything).Return(nil, plumbing.ErrReferenceNotFound)
	repository.EXPECT().Push(mock.Anything).Return(nil)

	mockComposer.EXPECT().CheckPlatformReqs(anyCtx, "/tmp").Return("", nil)
	mockComposer.EXPECT().Install(anyCtx, "/tmp").Return(nil)
	mockComposer.EXPECT().GetLockHash("/tmp").Return("dummy-hash", nil)
	mockComposer.EXPECT().Update(anyCtx, "/tmp", mock.Anything, mock.Anything, false, false).
		Return([]composer.PackageChange{{Package: "drupal/core", From: "9.0.0", To: "9.1.0"}}, nil)

	vcsProvider.EXPECT().GetUser(mock.Anything).Return("user", "mail")
	vcsProvider.EXPECT().CreateMergeRequest(anyCtx, mock.Anything, mock.Anything, mock.Anything, config.Branch).
		Return(codehosting.MergeRequest{}, nil)

	workflowService := NewWorkflowBaseService(zap.NewNop(), config, drush, vcsProvider, repositoryService, installer, mockComposer, event.NewManager(""))

	require.NoError(t, workflowService.StartUpdate(context.Background(), nil))

	// The baseline installs are the fan-out: independent per site, so they must overlap or the
	// concurrency the whole design rests on is not happening.
	assert.Greater(t, baseline.peak(), int32(1), "baseline site installs never overlapped")
	// The tail of updateSite commits into one shared working tree, so siteCommitMu has to keep
	// it to one site at a time however many are running.
	assert.EqualValues(t, 1, commitTail.peak(), "the committing tail of updateSite overlapped")

	installer.AssertExpectations(t)
	drush.AssertExpectations(t)
	repositoryService.AssertExpectations(t)
	mockComposer.AssertExpectations(t)
	vcsProvider.AssertExpectations(t)
}
