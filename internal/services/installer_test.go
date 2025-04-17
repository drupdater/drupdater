package services

import (
	"errors"
	"os"
	"testing"

	"github.com/drupdater/drupdater/internal/utils"

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

	repositoryURL := "https://example.com/repo.git"
	branch := "main"
	token := "token"
	sites := []string{"site1", "site2"}

	settingsService.On("ConfigureDatabase", "/tmp", "site1").Return(nil)
	settingsService.On("ConfigureDatabase", "/tmp", "site2").Return(nil)
	settingsService.On("RemoveProfile", "/tmp", "site1").Return(nil)
	settingsService.On("RemoveProfile", "/tmp", "site2").Return(nil)

	repositoryService.On("CloneRepository", repositoryURL, branch, token).Return(nil, nil, "/tmp", nil)

	t.Run("Success", func(t *testing.T) {
		commandExecutor := utils.NewMockCommandExecutor(t)
		commandExecutor.On("InstallDependencies", "/tmp").Return(nil)

		commandExecutor.On("InstallSite", "/tmp", "site1").Return(nil)
		commandExecutor.On("InstallSite", "/tmp", "site2").Return(nil)

		installer := &DefaultInstallerService{
			logger:          logger,
			repository:      repositoryService,
			commandExecutor: commandExecutor,
			settings:        settingsService,
		}
		err = installer.InstallDrupal(repositoryURL, branch, token, sites)
		if err != nil {
			t.Fatalf("Failed to install Drupal: %v", err)
		}

		commandExecutor.AssertExpectations(t)
	})

	t.Run("Failure", func(t *testing.T) {
		commandExecutor := utils.NewMockCommandExecutor(t)
		commandExecutor.On("InstallDependencies", "/tmp").Return(nil)
		commandExecutor.On("InstallSite", "/tmp", "site1").Return(nil)
		commandExecutor.On("InstallSite", "/tmp", "site2").Return(errors.New("failed to install site"))

		installer := &DefaultInstallerService{
			logger:          logger,
			repository:      repositoryService,
			commandExecutor: commandExecutor,
			settings:        settingsService,
		}
		err = installer.InstallDrupal(repositoryURL, branch, token, sites)
		if err == nil {
			t.Fatalf("Expected an error but got nil")
		}

		commandExecutor.AssertExpectations(t)
	})

	repositoryService.AssertExpectations(t)
	settingsService.AssertExpectations(t)
}
