package services

import (
	"errors"
	"os"
	"testing"

	"github.com/drupdater/drupdater/pkg/composer"
	"github.com/drupdater/drupdater/pkg/drush"
	"github.com/stretchr/testify/mock"

	"go.uber.org/zap"
)

func TestInstallDrupal(t *testing.T) {
	// Create a temporary file to act as the core.extension.yml
	tempFile, err := os.Create("/tmp/core.extension.yml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tempFile.Name())

	// Write initial YAML content to the temp file
	initialContent := `
module:
  existing_module: 0
`
	if _, err := tempFile.Write([]byte(initialContent)); err != nil {
		t.Fatalf("Failed to write initial content to temp file: %v", err)
	}

	// Close the temp file so it can be read by the function
	if err := tempFile.Close(); err != nil {
		t.Fatalf("Failed to close temp file: %v", err)
	}

	settingsService := NewMockSettingsService(t)
	repositoryService := NewMockRepositoryService(t)
	logger := zap.NewNop()
	composerService := composer.NewMockComposerService(t)

	repositoryURL := "https://example.com/repo.git"
	branch := "main"
	token := "token"
	sites := []string{"site1", "site2"}

	settingsService.On("ConfigureDatabase", mock.Anything, "/tmp", "site1").Return(nil)
	settingsService.On("ConfigureDatabase", mock.Anything, "/tmp", "site2").Return(nil)
	settingsService.On("RemoveProfile", mock.Anything, "/tmp", "site1").Return(nil)
	settingsService.On("RemoveProfile", mock.Anything, "/tmp", "site2").Return(nil)

	repositoryService.On("CloneRepository", repositoryURL, branch, token).Return(nil, nil, "/tmp", nil)

	t.Run("Success", func(t *testing.T) {
		drush := drush.NewMockDrushService(t)
		composerService.On("Install", mock.Anything, "/tmp").Return(nil)

		drush.On("InstallSite", mock.Anything, "/tmp", "site1").Return(nil)
		drush.On("InstallSite", mock.Anything, "/tmp", "site2").Return(nil)

		installer := &DefaultInstallerService{
			logger:     logger,
			repository: repositoryService,
			drush:      drush,
			settings:   settingsService,
			composer:   composerService,
		}
		err = installer.InstallDrupal(t.Context(), repositoryURL, branch, token, sites)
		if err != nil {
			t.Fatalf("Failed to install Drupal: %v", err)
		}

		drush.AssertExpectations(t)
	})

	t.Run("Failure", func(t *testing.T) {
		drush := drush.NewMockDrushService(t)
		composerService.On("Install", mock.Anything, "/tmp").Return(nil)
		drush.On("InstallSite", mock.Anything, "/tmp", "site1").Return(nil)
		drush.On("InstallSite", mock.Anything, "/tmp", "site2").Return(errors.New("failed to install site"))

		installer := &DefaultInstallerService{
			logger:     logger,
			repository: repositoryService,
			drush:      drush,
			settings:   settingsService,
			composer:   composerService,
		}
		err = installer.InstallDrupal(t.Context(), repositoryURL, branch, token, sites)
		if err == nil {
			t.Fatalf("Expected an error but got nil")
		}

		drush.AssertExpectations(t)
	})

	repositoryService.AssertExpectations(t)
	settingsService.AssertExpectations(t)
}
