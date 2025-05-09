package drupal

import (
	"context"
	"fmt"

	"github.com/drupdater/drupdater/internal"
	"github.com/drupdater/drupdater/internal/addon"
	"github.com/drupdater/drupdater/pkg/composer"
	"github.com/drupdater/drupdater/pkg/drush"
	git "github.com/go-git/go-git/v5"
	"github.com/gookit/event"
	"go.uber.org/zap"
)

type UpdaterService interface {
	UpdateDependencies(ctx context.Context, path string, worktree internal.Worktree, minimalChanges bool) error
	UpdateDrupal(ctx context.Context, path string, worktree internal.Worktree, site string) error
}

type DefaultUpdater struct {
	logger   *zap.Logger
	settings SettingsService
	config   internal.Config
	composer composer.Runner
	drush    drush.Runner
}

func NewDefaultUpdater(logger *zap.Logger, settings SettingsService, config internal.Config, composer composer.Runner, drush drush.Runner) *DefaultUpdater {

	return &DefaultUpdater{
		logger:   logger,
		settings: settings,
		config:   config,
		composer: composer,
		drush:    drush,
	}
}

func (us *DefaultUpdater) UpdateDependencies(ctx context.Context, path string, worktree internal.Worktree, minimalChanges bool) error {

	preComposerUpdateEvent := addon.NewPreComposerUpdateEvent(ctx, path, worktree, us.config, []string{}, []string{}, minimalChanges)
	err := event.FireEvent(preComposerUpdateEvent)
	if err != nil {
		return fmt.Errorf("failed to fire event: %w", err)
	}

	changes, err := us.composer.Update(ctx, path, preComposerUpdateEvent.PackagesToUpdate, preComposerUpdateEvent.PackagesToKeep, preComposerUpdateEvent.MinimalChanges, false)
	if err != nil {
		return fmt.Errorf("failed to update dependencies: %w", err)
	}
	if len(changes) == 0 {
		us.logger.Warn("no changes detected")
		return nil
	}

	postComposerUpdateEvent := addon.NewPostComposerUpdateEvent(ctx, path, worktree, us.config)
	err = event.FireEvent(postComposerUpdateEvent)
	if err != nil {
		return fmt.Errorf("failed to fire event: %w", err)
	}

	err = worktree.AddGlob("composer.*")
	if err != nil {
		return fmt.Errorf("failed to add composer.* files: %w", err)
	}
	if _, err := worktree.Commit("Update composer.json and composer.lock", &git.CommitOptions{}); err != nil {
		return fmt.Errorf("failed to commit composer.json and composer.lock: %w", err)
	}

	return nil
}

func (us *DefaultUpdater) UpdateDrupal(ctx context.Context, path string, worktree internal.Worktree, site string) error {

	us.logger.Info("updating site", zap.String("site", site))

	if err := us.settings.ConfigureDatabase(ctx, path, site); err != nil {
		return fmt.Errorf("failed to configure database: %w", err)
	}

	preSiteUpdateEvent := addon.NewPreSiteUpdateEvent(ctx, path, worktree, us.config, site)
	if err := event.FireEvent(preSiteUpdateEvent); err != nil {
		return fmt.Errorf("failed to fire event: %w", err)
	}

	if err := us.drush.UpdateSite(ctx, path, site); err != nil {
		return fmt.Errorf("failed to update site: %w", err)

	}

	if err := us.drush.ConfigResave(ctx, path, site); err != nil {
		return fmt.Errorf("failed to resave config: %w", err)

	}

	postSiteUpdateEvent := addon.NewPostSiteUpdateEvent(ctx, path, worktree, us.config, site)
	if err := event.FireEvent(postSiteUpdateEvent); err != nil {
		return fmt.Errorf("failed to fire event: %w", err)
	}

	us.logger.Info("export configuration", zap.String("site", site))
	if err := us.drush.ExportConfiguration(ctx, path, site); err != nil {
		return fmt.Errorf("failed to export configuration: %w", err)
	}

	return nil
}
