package drupal

import (
	"errors"
	"os"
	"testing"

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
	logger := zap.NewNop()

	settingsService.On("ConfigureDatabase", mock.Anything, "/tmp", "site1").Return(nil)
	settingsService.On("RemoveProfile", mock.Anything, "/tmp", "site1").Return(nil)

	t.Run("Success", func(t *testing.T) {
		drush := drush.NewMockRunner(t)

		drush.On("InstallSite", mock.Anything, "/tmp", "site1").Return(nil)

		installer := &DefaultInstallerService{
			logger:   logger,
			drush:    drush,
			settings: settingsService,
		}
		err = installer.Install(t.Context(), "/tmp", "site1")
		if err != nil {
			t.Fatalf("Failed to install Drupal: %v", err)
		}

		drush.AssertExpectations(t)
	})

	t.Run("Failure", func(t *testing.T) {
		drush := drush.NewMockRunner(t)
		drush.On("InstallSite", mock.Anything, "/tmp", "site1").Return(errors.New("failed to install site"))

		installer := &DefaultInstallerService{
			logger:   logger,
			drush:    drush,
			settings: settingsService,
		}
		err = installer.Install(t.Context(), "/tmp", "site1")
		if err == nil {
			t.Fatalf("Expected an error but got nil")
		}

		drush.AssertExpectations(t)
	})

	settingsService.AssertExpectations(t)
}
