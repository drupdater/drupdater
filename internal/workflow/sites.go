package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"

	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

// forEachSite runs fn per site concurrently, bounded by config.Concurrency, cancelling the rest
// on the first error. A site whose slot frees up only after that error never starts.
func (s services) forEachSite(ctx context.Context, fn func(context.Context, int, string) error) error {
	g, groupCtx := errgroup.WithContext(ctx)
	limit := s.config.Concurrency
	if limit <= 0 {
		limit = runtime.GOMAXPROCS(0)
	}
	g.SetLimit(limit)
	for i, site := range s.config.Sites {
		g.Go(func() error {
			if err := groupCtx.Err(); err != nil {
				// mutator-disable-next-line statement/return equivalent: the group keeps its first error
				return err
			}
			return fn(groupCtx, i, site)
		})
	}
	return g.Wait()
}

// updateSites updates every site's database concurrently, then writes each site's changes into
// the working tree one site at a time, in --sites order: the sites share one git index, and two
// may name the same config or translation directory.
func (r *run) updateSites(ctx context.Context) (err error) {
	updated := make([]json.RawMessage, len(r.config.Sites))
	configured := make([]bool, len(r.config.Sites))
	// Uncancelled: the first failing site cancels ctx, and every site must still be put back.
	defer func() {
		for i, site := range r.config.Sites {
			if !configured[i] {
				continue
			}
			if restoreErr := r.sites.RestoreSettings(context.WithoutCancel(ctx), r.path, site); restoreErr != nil {
				err = errors.Join(err, fmt.Errorf("failed to restore settings of %s: %w", site, restoreErr))
			}
		}
	}()

	if err := r.forEachSite(ctx, func(ctx context.Context, i int, site string) error {
		if err := r.sites.ConfigureDatabase(ctx, r.path, site); err != nil {
			return fmt.Errorf("failed to configure database: %w", err)
		}
		configured[i] = true
		facts, err := r.updateDatabase(ctx, site)
		updated[i] = facts
		return err
	}); err != nil {
		return err
	}

	for i, site := range r.config.Sites {
		if err := r.commitSiteChanges(ctx, site, updated[i]); err != nil {
			return err
		}
	}
	return nil
}

// updateDatabase runs the site's update hooks and returns its facts as they stand afterwards.
func (r *run) updateDatabase(ctx context.Context, site string) (json.RawMessage, error) {
	r.logger.Info("updating site", zap.String("site", site))

	facts, err := r.sites.Facts(ctx, r.path, site)
	if err != nil {
		return nil, err
	}

	if _, err := r.fire(ctx, preSiteUpdate(site, facts)); err != nil {
		return nil, err
	}

	return r.sites.Update(ctx, r.path, site)
}

func (r *run) commitSiteChanges(ctx context.Context, site string, facts json.RawMessage) error {
	if _, err := r.fire(ctx, postSiteUpdate(site, facts)); err != nil {
		return err
	}

	r.logger.Info("exporting configuration", zap.String("site", site))
	exported, err := r.sites.ExportConfiguration(ctx, r.path, site)
	if err != nil {
		return err
	}
	// The committer reads an empty path as the whole tree, which would sweep in unrelated changes.
	if exported == "" {
		return fmt.Errorf("failed to locate the exported configuration: site %s has no config sync directory", site)
	}
	if _, err := r.commit(ctx, "Update configuration "+site, exported); err != nil {
		return fmt.Errorf("failed to commit the exported configuration: %w", err)
	}

	return nil
}
