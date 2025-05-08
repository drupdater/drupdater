package drupal

import (
	"context"

	"github.com/drupdater/drupdater/pkg/drush"

	"go.uber.org/zap"
)

type InstallerService interface {
	Install(ctx context.Context, path string, site string) error
}

type DefaultInstallerService struct {
	logger   *zap.Logger
	drush    drush.Runner
	settings SettingsService
}

func NewDefaultInstallerService(logger *zap.Logger, drush drush.Runner, settings SettingsService) *DefaultInstallerService {
	return &DefaultInstallerService{
		logger:   logger,
		drush:    drush,
		settings: settings,
	}
}

func (is *DefaultInstallerService) Install(ctx context.Context, path string, site string) error {

	is.logger.Info("installing site", zap.String("site", site))

	if err := is.settings.ConfigureDatabase(ctx, path, site); err != nil {
		return err
	}

	if err := is.settings.RemoveProfile(ctx, path, site); err != nil {
		return err

	}

	if err := is.drush.InstallSite(ctx, path, site); err != nil {
		return err
	}

	return nil
}
