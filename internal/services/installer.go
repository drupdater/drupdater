package services

import (
	"context"
	"runtime"
	"slices"
	"sync"

	"github.com/drupdater/drupdater/pkg/composer"
	"github.com/drupdater/drupdater/pkg/drush"

	"go.uber.org/zap"
)

type InstallerService interface {
	InstallDrupal(ctx context.Context, repositoryURL string, branch string, token string, sites []string) error
}

type DefaultInstallerService struct {
	logger     *zap.Logger
	repository RepositoryService
	drush      drush.DrushService
	settings   SettingsService
	composer   composer.ComposerService
}

func newDefaultInstallerService(logger *zap.Logger, repository RepositoryService, drush drush.DrushService, settings SettingsService, composer composer.ComposerService) *DefaultInstallerService {
	return &DefaultInstallerService{
		logger:     logger,
		repository: repository,
		drush:      drush,
		settings:   settings,
		composer:   composer,
	}
}

func (is *DefaultInstallerService) InstallDrupal(ctx context.Context, repositoryURL string, branch string, token string, sites []string) error {

	is.logger.Info("cloning repository for site-install", zap.String("repositoryURL", repositoryURL), zap.String("branch", branch))
	_, _, path, err := is.repository.CloneRepository(repositoryURL, branch, token)
	if err != nil {
		is.logger.Error("failed to clone repository", zap.String("repositoryURL", repositoryURL), zap.String("branch", branch), zap.Error(err))
		return err
	}

	if err = is.composer.Install(ctx, path); err != nil {
		return err
	}

	var wg sync.WaitGroup

	maxProcs := runtime.NumCPU() - 1 // Leave one CPU free for the composer update process.
	for chunk := range slices.Chunk(sites, maxProcs) {
		errChannel := make(chan error, len(chunk))

		for _, site := range chunk {
			wg.Add(1)

			go func(site string) {
				defer wg.Done()

				is.logger.Info("installing site", zap.String("site", site))

				if err = is.settings.ConfigureDatabase(ctx, path, site); err != nil {
					errChannel <- err
					return
				}

				if err = is.settings.RemoveProfile(ctx, path, site); err != nil {
					errChannel <- err
					return
				}

				if err = is.drush.InstallSite(ctx, path, site); err != nil {
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
