package workflow

import (
	"context"
	"errors"
	"fmt"
	"runtime"

	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

// forEachSite runs fn per site concurrently, bounded by config.Concurrency, cancelling the rest
// on the first error. Sites start in config order, which updateSite's turns rely on.
func (s services) forEachSite(ctx context.Context, fn func(context.Context, int, string) error) error {
	g, groupCtx := errgroup.WithContext(ctx)
	limit := s.config.Concurrency
	if limit <= 0 {
		limit = runtime.GOMAXPROCS(0)
	}
	g.SetLimit(limit)
	for i, site := range s.config.Sites {
		g.Go(func() error {
			return fn(groupCtx, i, site)
		})
	}
	return g.Wait()
}

// updateSites runs updateSite for every site, handing each its turn: turns[i] closes once site i
// may write into the working tree, which happens only after every earlier site has.
func (r *run) updateSites(ctx context.Context) error {
	turns := make([]chan struct{}, len(r.config.Sites)+1)
	for i := range turns {
		turns[i] = make(chan struct{})
	}
	close(turns[0])
	return r.forEachSite(ctx, func(ctx context.Context, i int, site string) error {
		return r.updateSite(ctx, site, turns[i], turns[i+1])
	})
}

// updateSite updates the site's database concurrently with the other sites, then waits for its
// turn to write into the shared working tree: two sites may share a config or translation
// directory, and the commits land in --sites order however the sites finish.
func (r *run) updateSite(ctx context.Context, site string, turn <-chan struct{}, next chan<- struct{}) (err error) {
	r.logger.Info("updating site", zap.String("site", site))

	if err := r.installer.ConfigureDatabase(ctx, r.path, site); err != nil {
		return fmt.Errorf("failed to configure database: %w", err)
	}
	// Uncancelled: the first failing site cancels ctx, and the rest must still be put back.
	defer func() {
		if restoreErr := r.installer.RestoreSettings(context.WithoutCancel(ctx), r.path, site); restoreErr != nil {
			err = errors.Join(err, fmt.Errorf("failed to restore settings: %w", restoreErr))
		}
	}()

	preSiteUpdateEvent := NewPreSiteUpdateEvent(ctx, r.path, r.committer, site)
	if err := Fire(r.hooks, preSiteUpdateEvent); err != nil {
		return err
	}

	if err := r.drush.UpdateSite(ctx, r.path, site); err != nil {
		return fmt.Errorf("failed to update site: %w", err)

	}

	if err := r.drush.ConfigResave(ctx, r.path, site); err != nil {
		return fmt.Errorf("failed to resave config: %w", err)

	}

	select {
	case <-turn:
	case <-ctx.Done():
		return ctx.Err()
	}
	if err := r.commitSiteChanges(ctx, site); err != nil {
		return err
	}
	close(next)
	return nil
}

func (r *run) commitSiteChanges(ctx context.Context, site string) error {
	postSiteUpdateEvent := NewPostSiteUpdateEvent(ctx, r.path, r.committer, site)
	if err := Fire(r.hooks, postSiteUpdateEvent); err != nil {
		return err
	}

	r.logger.Info("exporting configuration", zap.String("site", site))
	if err := r.drush.ExportConfiguration(ctx, r.path, site); err != nil {
		return fmt.Errorf("failed to export configuration: %w", err)
	}
	configDir, err := r.drush.GetConfigSyncDir(ctx, r.path, site, true)
	if err != nil {
		return fmt.Errorf("failed to locate the exported configuration: %w", err)
	}
	// The committer reads an empty path as the whole tree, which would sweep in unrelated changes.
	if configDir == "" {
		return fmt.Errorf("failed to locate the exported configuration: site %s has no config sync directory", site)
	}
	if _, err := r.committer.Commit("Update configuration "+site, configDir); err != nil {
		return fmt.Errorf("failed to commit the exported configuration: %w", err)
	}

	return nil
}
