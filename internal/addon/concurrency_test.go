package addon

import (
	"context"
	"strconv"
	"sync"
	"testing"

	"github.com/drupdater/drupdater/internal/services"
	"github.com/drupdater/drupdater/pkg/drush"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// The per-site events fire from errgroup goroutines, one per site, so every addon accumulating
// state across sites writes its map concurrently. These tests are the ones that give `-race`
// something to observe: the rest of the suite drives each handler with a single site, where an
// unguarded map write is invisible. Each also asserts no site is lost, which stands on its own.

// concurrentSites returns the site names to drive the handlers with. Enough of them that a lost
// update is not masked by the goroutines happening to serialise.
func concurrentSites(n int) []string {
	sites := make([]string, 0, n)
	for i := range n {
		sites = append(sites, "site"+strconv.Itoa(i))
	}
	return sites
}

// runPerSite fires fn for every site at once and waits for all of them.
func runPerSite(t *testing.T, sites []string, fn func(site string) error) {
	t.Helper()

	var wg sync.WaitGroup
	start := make(chan struct{})
	for _, site := range sites {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			assert.NoError(t, fn(site))
		}()
	}
	// Released together, so the handlers overlap instead of finishing as they are spawned.
	close(start)
	wg.Wait()
}

func TestUpdateHooksRecordsEverySiteUnderConcurrency(t *testing.T) {
	mockDrush := NewMockDrush(t)
	updateHooks := NewUpdateHooks(zap.NewNop(), mockDrush)

	ctx := context.Background()
	worktree := NewMockWorktree(t)
	sites := concurrentSites(8)

	for _, site := range sites {
		mockDrush.EXPECT().GetUpdateHooks(ctx, "/project", site).
			Return(map[string]drush.UpdateHook{site + "_update_8001": {Module: site}}, nil)
	}

	runPerSite(t, sites, func(site string) error {
		return updateHooks.preSiteUpdateHandler(services.NewPreSiteUpdateEvent(ctx, "/project", worktree, site))
	})

	require.Len(t, updateHooks.hooks, len(sites))
	for _, site := range sites {
		assert.Contains(t, updateHooks.hooks, site)
	}
}

func TestUnsupportedModulesRecordsEverySiteUnderConcurrency(t *testing.T) {
	mockDrush := NewMockDrush(t)
	um := NewUnsupportedModules(zap.NewNop(), mockDrush)

	ctx := context.Background()
	worktree := NewMockWorktree(t)
	sites := concurrentSites(8)

	// A module per site plus one they all report, so the run exercises both a fresh key and the
	// deduplicating overwrite while the goroutines overlap.
	for _, site := range sites {
		mockDrush.EXPECT().GetUnsupportedModules(ctx, "/project", site).Return([]drush.UnsupportedModule{
			{Name: "module_" + site, InstalledVersion: "1.0.0", RecommendedVersion: "2.0.0"},
			{Name: "shared_module", InstalledVersion: "1.0.0", RecommendedVersion: "None"},
		}, nil)
	}

	runPerSite(t, sites, func(site string) error {
		return um.preSiteUpdateHandler(services.NewPreSiteUpdateEvent(ctx, "/project", worktree, site))
	})

	require.Len(t, um.modules, len(sites)+1)
	assert.Contains(t, um.modules, "shared_module")
	for _, site := range sites {
		assert.Contains(t, um.modules, "module_"+site)
	}
}

func TestTranslationsUpdaterRecordsEverySiteUnderConcurrency(t *testing.T) {
	mockDrush := NewMockDrush(t)
	tu := NewTranslationsUpdater(zap.NewNop(), mockDrush, NewMockRepository(t))

	ctx := context.Background()
	worktree := NewMockWorktree(t)
	sites := concurrentSites(8)

	// The skip path is the one that writes without touching the worktree, so the goroutines
	// reach `record` with nothing else serialising them.
	for _, site := range sites {
		mockDrush.EXPECT().IsModuleEnabled(ctx, "/project", site, "locale_deploy").Return(false, nil)
	}

	runPerSite(t, sites, func(site string) error {
		return tu.postSiteUpdateHandler(services.NewPostSiteUpdateEvent(ctx, "/project", worktree, site))
	})

	require.Len(t, tu.results, len(sites))
	for _, site := range sites {
		assert.Contains(t, tu.results, site)
	}
}
