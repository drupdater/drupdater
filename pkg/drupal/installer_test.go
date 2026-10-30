package drupal

import (
	"context"
	"errors"
	"testing"

	"github.com/spf13/afero"
	"gopkg.in/yaml.v3"

	"go.uber.org/zap"
)

func TestInstallDrupal(t *testing.T) {
	// Setup common test data
	logger := zap.NewNop()
	fs := afero.NewMemMapFs()

	coreExtensionContent := `
_core:
  default_config_hash: 4GIX5Esnc_umpXUBj4IIocRX7Mt5fPhm4AgXfE3E56E
module:
  access_unpublished: 0
  admin_toolbar: 0
  admin_toolbar_links_access_filter: 0
  sqlite: 0
  thunder: 1000
theme:
  gin: 0
profile: thunder
`
	configSyncDir := "/tmp/config/sync"
	coreExtensionPath := configSyncDir + "/core.extension.yml"

	if err := afero.WriteFile(fs, coreExtensionPath, []byte(coreExtensionContent), 0644); err != nil {
		t.Fatalf("Failed to write initial content to core.extension.yml: %v", err)
	}

	settingsContent := ""
	settingsPath := "/tmp/web/sites/site1/settings.php"
	if err := afero.WriteFile(fs, settingsPath, []byte(settingsContent), 0644); err != nil {
		t.Fatalf("Failed to write initial content to settings.php: %v", err)
	}

	t.Run("Success", func(t *testing.T) {
		// Setup mocks
		var duringInstall []byte
		drush := NewMockDrush(t)
		drush.EXPECT().InstallSite(t.Context(), "/tmp", "site1").RunAndReturn(func(context.Context, string, string) error {
			var err error
			duringInstall, err = afero.ReadFile(fs, settingsPath)
			return err
		})
		drush.EXPECT().GetConfigSyncDir(t.Context(), "/tmp", "site1").Return(configSyncDir, nil)

		composer := NewMockComposer(t)
		composer.EXPECT().GetConfig(t.Context(), "/tmp", "extra.drupal-scaffold.locations.web-root").Return("web", nil)

		installer := &Installer{
			logger:   logger,
			drush:    drush,
			composer: composer,
			fs:       fs,
			dataDir:  "/data",
		}

		err := installer.Install(t.Context(), "/tmp", "site1")
		if err != nil {
			t.Fatalf("Failed to install Drupal: %v", err)
		}

		updatedContent := `
` + settingsMarker + `
$databases['default']['default'] = [
	'database' => '/data/site1.sqlite',
	'prefix' => '',
	'driver' => 'sqlite',
	'namespace' => 'Drupal\\sqlite\\Driver\\Database\\sqlite',
	'autoload' => 'core/modules/sqlite/src/Driver/Database/sqlite/',
];
$settings['skip_permissions_hardening'] = TRUE;
$settings['file_private_path'] = '/data/private/site1';
$settings['hash_salt'] = 'changeme';
`

		if string(duringInstall) != updatedContent {
			t.Fatalf("Expected content during install: %s, got: %s", updatedContent, string(duringInstall))
		}

		after, err := afero.ReadFile(fs, settingsPath)
		if err != nil {
			t.Fatalf("Failed to read settings.php: %v", err)
		}
		if string(after) != settingsContent {
			t.Fatalf("settings.php must be restored after the install, got: %s", string(after))
		}
	})

	t.Run("Failure", func(t *testing.T) {
		// Setup mocks
		drush := NewMockDrush(t)
		drush.EXPECT().InstallSite(t.Context(), "/tmp", "site1").Return(errors.New("failed to install site"))
		drush.EXPECT().GetConfigSyncDir(t.Context(), "/tmp", "site1").Return(configSyncDir, nil)

		composer := NewMockComposer(t)
		composer.EXPECT().GetConfig(t.Context(), "/tmp", "extra.drupal-scaffold.locations.web-root").Return("web", nil)

		installer := &Installer{
			logger:   logger,
			drush:    drush,
			composer: composer,
			fs:       fs,
		}

		err := installer.Install(t.Context(), "/tmp", "site1")

		if err == nil {
			t.Fatalf("Expected an error but got nil")
		}

		after, readErr := afero.ReadFile(fs, settingsPath)
		if readErr != nil {
			t.Fatalf("Failed to read settings.php: %v", readErr)
		}
		if string(after) != settingsContent {
			t.Fatalf("settings.php must be restored after a failed install, got: %s", string(after))
		}
	})
}

func TestAddSqliteModule(t *testing.T) {
	logger := zap.NewNop()
	drush := NewMockDrush(t)
	fs := afero.NewMemMapFs()

	// Create a temporary file to act as the core.extension.yml
	tempFile, err := fs.Create("/tmp/core.extension.yml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}

	// Write initial YAML content to the temp file
	initialContent := `
_core:
  default_config_hash: 4GIX5Esnc_umpXUBj4IIocRX7Mt5fPhm4AgXfE3E56E
module:
  access_unpublished: 0
  admin_toolbar: 0
  admin_toolbar_links_access_filter: 0
  thunder: 1000
theme:
  gin: 0
profile: thunder
`
	if _, err := tempFile.Write([]byte(initialContent)); err != nil {
		t.Fatalf("Failed to write initial content to temp file: %v", err)
	}

	// Close the temp file so it can be read by the function
	if err := tempFile.Close(); err != nil {
		t.Fatalf("Failed to close temp file: %v", err)
	}

	drush.EXPECT().GetConfigSyncDir(t.Context(), "/tmp", "default").Return("/tmp", nil)

	installer := &Installer{
		logger: logger,
		drush:  drush,
		fs:     fs,
	}

	if err := installer.addSqliteModule(t.Context(), "/tmp", "default"); err != nil {
		t.Fatalf("Failed to add SQLite module: %v", err)
	}

	updatedContent, err := afero.ReadFile(fs, tempFile.Name())
	if err != nil {
		t.Fatalf("Failed to read updated content from temp file: %v", err)
	}

	// Unmarshal the updated content
	var updatedConfig map[string]any
	if err := yaml.Unmarshal(updatedContent, &updatedConfig); err != nil {
		t.Fatalf("Failed to unmarshal updated content: %v", err)
	}

	// Check if the SQLite module was added correctly
	modules, ok := updatedConfig["module"].(map[string]any)
	if !ok {
		t.Fatalf("Modules key is not a map")
	}

	if _, exists := modules["sqlite"]; !exists {
		t.Fatalf("SQLite module was not added")
	}

	if modules["sqlite"] != 0 {
		t.Fatalf("SQLite module value is not 0")
	}
}

func TestRemoveProfile(t *testing.T) {
	logger := zap.NewNop()
	drush := NewMockDrush(t)
	fs := afero.NewMemMapFs()

	// Create a temporary file to act as the core.extension.yml
	tempFile, err := fs.Create("/tmp/core.extension.yml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}

	// Write initial YAML content to the temp file
	initialContent := `
_core:
  default_config_hash: 4GIX5Esnc_umpXUBj4IIocRX7Mt5fPhm4AgXfE3E56E
module:
  access_unpublished: 0
  admin_toolbar: 0
  admin_toolbar_links_access_filter: 0
  standard: 1000
theme:
  gin: 0
profile: standard
`
	if _, err := tempFile.Write([]byte(initialContent)); err != nil {
		t.Fatalf("Failed to write initial content to temp file: %v", err)
	}

	// Close the temp file so it can be read by the function
	if err := tempFile.Close(); err != nil {
		t.Fatalf("Failed to close temp file: %v", err)
	}

	drush.EXPECT().GetConfigSyncDir(t.Context(), "/tmp", "default").Return("/tmp", nil)

	installer := &Installer{
		logger: logger,
		drush:  drush,
		fs:     fs,
	}

	if err := installer.RemoveProfile(t.Context(), "/tmp", "default"); err != nil {
		t.Fatalf("Failed to remove profile: %v", err)
	}

	expectedContent := `
_core:
  default_config_hash: 4GIX5Esnc_umpXUBj4IIocRX7Mt5fPhm4AgXfE3E56E
module:
  access_unpublished: 0
  admin_toolbar: 0
  admin_toolbar_links_access_filter: 0
theme:
  gin: 0
`

	updatedContent, err := afero.ReadFile(fs, tempFile.Name())
	if err != nil {
		t.Fatalf("Failed to read updated content from temp file: %v", err)
	}

	if string(updatedContent) != expectedContent {
		t.Fatalf("Expected content: %s, got: %s", expectedContent, string(updatedContent))
	}
}
