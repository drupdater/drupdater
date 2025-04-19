package drupal

import (
	"context"
	"runtime"
	"slices"
	"sync"

	"github.com/drupdater/drupdater/pkg/drush"

	"go.uber.org/zap"
)

type InstallerService interface {
	InstallDrupal(ctx context.Context, path string, sites []string) error
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

func (is *DefaultInstallerService) InstallDrupal(ctx context.Context, path string, sites []string) error {

	var wg sync.WaitGroup

	maxProcs := runtime.NumCPU() - 1 // Leave one CPU free for the composer update process.
	for chunk := range slices.Chunk(sites, maxProcs) {
		errChannel := make(chan error, len(chunk))

		for _, site := range chunk {
			wg.Add(1)

			go func(site string) {
				defer wg.Done()

				is.logger.Info("installing site", zap.String("site", site))

				if err := is.settings.ConfigureDatabase(ctx, path, site); err != nil {
					errChannel <- err
					return
				}

				if err := is.settings.RemoveProfile(ctx, path, site); err != nil {
					errChannel <- err
					return
				}

				if err := is.drush.InstallSite(ctx, path, site); err != nil {
					errChannel <- err
					return
				}

			}(site)
		}
		wg.Wait()

		close(errChannel)

		for err := range errChannel {
			if err != nil {
				return err
			}
		}
	}

	return nil
}
