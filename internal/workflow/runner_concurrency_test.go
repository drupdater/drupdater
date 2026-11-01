package workflow

import (
	"context"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/drupdater/drupdater/internal/repo"

	"github.com/drupdater/drupdater/internal/codehosting"
	"github.com/drupdater/drupdater/internal/composer"
	"github.com/drupdater/drupdater/internal/config"
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

// Every other Run test runs a single site, which walks the per-site phases on one
// goroutine and leaves forEachSite's fan-out and the in-order commits untested against each other. This
// one drives the real workflow with several sites, and asserts both halves of the design: the
// independent phase overlaps, the committing one does not.
func TestRunWithConcurrentSites(t *testing.T) {
	installer := NewMockInstaller(t)
	repositoryService := NewMockGitService(t)
	vcsProvider := NewMockPlatform(t)
	repository := NewMockGitRepository(t)
	expectUpdateBranch(repository)
	mockComposer := NewMockComposer(t)
	expectVersionLookup(mockComposer)
	drush := NewMockDrush(t)

	sites := []string{"site1", "site2", "site3", "site4"}
	config := config.Config{
		RepositoryURL: "https://example.com/repo.git",
		Branch:        "main",
		Token:         "token",
		Clone:         true,
		Sites:         sites,
		// Unbounded, so the sites genuinely overlap rather than running one at a time.
		Concurrency: len(sites),
	}

	worktree := NewMockWorktree(t)
	worktree.EXPECT().Commit(anyCtx, mock.Anything, mock.Anything).Return(nil)
	worktree.EXPECT().StageGlob(anyCtx, mock.Anything).Return(nil)
	worktree.EXPECT().Unstaged(anyCtx).Return(nil, nil).Maybe()
	worktree.EXPECT().Staged(anyCtx).Return(nil, nil).Maybe()

	// Two opposite invariants, measured by peak occupancy rather than by the race detector: the
	// git index the commit tail writes is mocked here, so nothing about it is shared memory.
	baseline := newOccupancyProbe()
	commitTail := newOccupancyProbe()

	for _, site := range sites {
		installer.EXPECT().Install(anyCtx, testCheckout, site).RunAndReturn(baseline.probe)
		installer.EXPECT().ConfigureDatabase(anyCtx, testCheckout, site).Return(nil)
		installer.EXPECT().RestoreSettings(anyCtx, testCheckout, site).Return(nil)
		drush.EXPECT().Facts(anyCtx, testCheckout, site).Return(syncFacts(testCheckout+"/config/"+site), nil)
		drush.EXPECT().UpdateSite(anyCtx, testCheckout, site).Return(nil)
		drush.EXPECT().ExportConfiguration(anyCtx, testCheckout, site).RunAndReturn(commitTail.probe)
		worktree.EXPECT().Stage(anyCtx, []string{"config/" + site}).Return(nil)
		drush.EXPECT().ConfigResave(anyCtx, testCheckout, site).Return(nil)
		drush.EXPECT().LocalFacts(anyCtx, testCheckout, site).Return(syncFacts(testCheckout+"/config/"+site), nil)
	}

	repositoryService.EXPECT().CloneRepository(anyCtx, config.RepositoryURL, config.Branch, config.Token, repo.Identity{Name: "user", Email: "mail"}).Return(repo.Checkout{Repository: repository, Worktree: worktree, Path: testCheckout, Author: testAuthor}, nil)
	repositoryService.EXPECT().IsShallowClone(anyCtx, testCheckout).Return(false, nil)
	repository.EXPECT().RemoteBranchExists(anyCtx, mock.Anything, mock.Anything).Return(false, nil)
	repository.EXPECT().BranchExists(anyCtx, mock.Anything).Return(false, nil)
	repository.EXPECT().Push(anyCtx, mock.Anything, mock.Anything).Return(nil)

	mockComposer.EXPECT().CheckPlatformReqs(anyCtx, testCheckout).Return("", nil)

	mockComposer.EXPECT().CheckAgent(anyCtx, testCheckout).Return(nil)
	mockComposer.EXPECT().Install(anyCtx, testCheckout).Return(nil)
	mockComposer.EXPECT().Project(anyCtx, testCheckout).Return(composer.Project{WebRoot: "web", LockHash: "dummy-hash"}, nil)
	mockComposer.EXPECT().Update(anyCtx, testCheckout, mock.Anything, mock.Anything, false, false).
		Return([]composer.PackageChange{{Package: "drupal/core", From: "9.0.0", To: "9.1.0"}}, nil)

	vcsProvider.EXPECT().GetUser(mock.Anything).Return("user", "mail")
	vcsProvider.EXPECT().CreateMergeRequest(anyCtx, mock.Anything, mock.Anything, mock.Anything, config.Branch).
		Return(codehosting.MergeRequest{}, nil)

	workflowService := New(zap.NewNop(), config, drush, vcsProvider, repositoryService, installer, mockComposer)

	require.NoError(t, workflowService.Run(context.Background(), nil))

	// The baseline installs are the fan-out: independent per site, so they must overlap or the
	// concurrency the whole design rests on is not happening.
	assert.Greater(t, baseline.peak(), int32(1), "baseline site installs never overlapped")
	// The sites commit into one shared working tree, so that must stay one site at a time
	// however many are running.
	assert.EqualValues(t, 1, commitTail.peak(), "the sites' commits overlapped")
}

// sitesRunner is a Runner whose sites each export to config/<site>, and which records the order
// the exports happen in. stub sets what each site's update hooks do.
func sitesRunner(t *testing.T, sites []string, concurrency int, stub func(site string) error) (*run, *fakeCommitter, func() []string) {
	t.Helper()
	installer := NewMockInstaller(t)
	drush := NewMockDrush(t)
	var mu sync.Mutex
	var exported []string

	for _, site := range sites {
		installer.EXPECT().ConfigureDatabase(anyCtx, testCheckout, site).Return(nil)
		installer.EXPECT().RestoreSettings(anyCtx, testCheckout, site).Return(nil).Once()
		drush.EXPECT().Facts(anyCtx, testCheckout, site).Return(syncFacts(testCheckout+"/config/"+site), nil)
		drush.EXPECT().UpdateSite(anyCtx, testCheckout, site).RunAndReturn(func(context.Context, string, string) error {
			return stub(site)
		})
		drush.EXPECT().ConfigResave(anyCtx, testCheckout, site).Return(nil).Maybe()
		drush.EXPECT().LocalFacts(anyCtx, testCheckout, site).Return(syncFacts(testCheckout+"/config/"+site), nil).Maybe()
		drush.EXPECT().ExportConfiguration(anyCtx, testCheckout, site).RunAndReturn(func(context.Context, string, string) error {
			mu.Lock()
			defer mu.Unlock()
			exported = append(exported, site)
			return nil
		}).Maybe()
	}

	committer := &fakeCommitter{result: true}
	r := testRun(services{
		logger:    zap.NewNop(),
		config:    config.Config{Sites: sites, Concurrency: concurrency},
		installer: installer,
		drush:     drush,
	}, repo.Checkout{Path: testCheckout})
	r.committer = committer
	return r, committer, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(exported)
	}
}

func commitMessages(c *fakeCommitter) []string {
	messages := make([]string, 0, len(c.commits))
	for _, commit := range c.commits {
		messages = append(messages, commit.message)
	}
	return messages
}

// withinDeadline fails instead of hanging when the sites deadlock.
func withinDeadline(t *testing.T, fn func() error) error {
	t.Helper()
	result := make(chan error, 1)
	go func() { result <- fn() }()
	select {
	case err := <-result:
		return err
	case <-time.After(10 * time.Second):
		t.Fatal("the sites never finished")
		return nil
	}
}

func TestUpdateSitesCommitsInSiteOrderHoweverTheSitesFinish(t *testing.T) {
	sites := []string{"site1", "site2", "site3"}
	// Each site's update hooks finish only after the next site's have, so the sites finish their
	// database work last to first.
	finished := map[string]chan struct{}{}
	for _, site := range sites {
		finished[site] = make(chan struct{})
	}
	r, committer, exported := sitesRunner(t, sites, len(sites), func(site string) error {
		defer close(finished[site])
		if i := slices.Index(sites, site); i+1 < len(sites) {
			<-finished[sites[i+1]]
		}
		return nil
	})

	require.NoError(t, withinDeadline(t, func() error { return r.updateSites(t.Context()) }))

	assert.Equal(t, sites, exported())
	assert.Equal(t, []string{"Update configuration site1", "Update configuration site2", "Update configuration site3"}, commitMessages(committer))
}

func TestUpdateSitesRunsInOrderWithOneSiteAtATime(t *testing.T) {
	sites := []string{"site1", "site2", "site3"}
	r, committer, exported := sitesRunner(t, sites, 1, func(string) error { return nil })

	require.NoError(t, withinDeadline(t, func() error { return r.updateSites(t.Context()) }))

	assert.Equal(t, sites, exported())
	assert.Len(t, committer.commits, len(sites))
}

func TestUpdateSitesWritesNothingAfterAnySiteFails(t *testing.T) {
	sites := []string{"site1", "site2", "site3"}
	// site1 fails only once the later sites have finished their database work, so a later site
	// writing regardless would show up as an export.
	waiting := make(chan struct{}, len(sites))
	r, committer, exported := sitesRunner(t, sites, len(sites), func(site string) error {
		if site != "site1" {
			waiting <- struct{}{}
			return nil
		}
		<-waiting
		<-waiting
		return assert.AnError
	})

	err := withinDeadline(t, func() error { return r.updateSites(t.Context()) })

	require.ErrorIs(t, err, assert.AnError)
	assert.Empty(t, exported(), "a site exported although another failed")
	assert.Empty(t, committer.commits)
}
