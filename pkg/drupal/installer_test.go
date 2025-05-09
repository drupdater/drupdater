package drupal

import (
	"errors"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/mock"
	"gopkg.in/yaml.v3"

	"go.uber.org/zap"
)

func TestInstallDrupal(t *testing.T) {

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
		drush := NewMockDrush(t)
		drush.EXPECT().InstallSite(mock.Anything, "/tmp", "site1").Return(nil)
		drush.EXPECT().GetConfigSyncDir(mock.Anything, "/tmp", "site1", false).Return(configSyncDir, nil)

		composer := NewMockComposer(t)
		composer.EXPECT().GetConfig(mock.Anything, "/tmp", "extra.drupal-scaffold.locations.web-root").Return("web", nil)

		installer := &Installer{
			logger:   logger,
			drush:    drush,
			composer: composer,
			fs:       fs,
		}
		err := installer.Install(t.Context(), "/tmp", "site1")
		if err != nil {
			t.Fatalf("Failed to install Drupal: %v", err)
		}

		// Read the updated file content
		settingsContent, err := afero.ReadFile(fs, settingsPath)
		if err != nil {
			t.Fatalf("Failed to read updated content from settings.php: %v", err)
		}
		updatedContent := `
$databases['default']['default'] = [
	'database' => '/site1.sqlite',
	'prefix' => '',
	'driver' => 'sqlite',
	'namespace' => 'Drupal\\sqlite\\Driver\\Database\\sqlite',
	'autoload' => 'core/modules/sqlite/src/Driver/Database/sqlite/',
];
$settings['skip_permissions_hardening'] = TRUE;
$settings['file_private_path'] = '/private/site1';
$settings['hash_salt'] = 'changeme';
`

		// Check if the SQLite module was added correctly
		if string(settingsContent) != updatedContent {
			t.Fatalf("Expected content: %s, got: %s", updatedContent, settingsContent)
		}

		drush.AssertExpectations(t)
		composer.AssertExpectations(t)
	})

	t.Run("Failure", func(t *testing.T) {
		drush := NewMockDrush(t)
		drush.EXPECT().InstallSite(mock.Anything, "/tmp", "site1").Return(errors.New("failed to install site"))
		drush.EXPECT().GetConfigSyncDir(mock.Anything, "/tmp", "site1", false).Return(configSyncDir, nil)

		composer := NewMockComposer(t)
		composer.EXPECT().GetConfig(mock.Anything, "/tmp", "extra.drupal-scaffold.locations.web-root").Return("web", nil)

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

		drush.AssertExpectations(t)
		composer.AssertExpectations(t)
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

	installer := &Installer{
		logger: logger,
		drush:  drush,
		fs:     fs,
	}

	drush.On("GetConfigSyncDir", mock.Anything, "/tmp", "default", false).Return("/tmp", nil)

	// Call the function to add the SQLite module
	if err := installer.addSqliteModule(t.Context(), "/tmp", "default"); err != nil {
		t.Fatalf("Failed to add SQLite module: %v", err)
	}

	// Read the updated file content
	updatedContent, err := afero.ReadFile(fs, tempFile.Name())
	if err != nil {
		t.Fatalf("Failed to read updated content from temp file: %v", err)
	}

	// Unmarshal the updated content
	var updatedConfig map[string]interface{}
	if err := yaml.Unmarshal(updatedContent, &updatedConfig); err != nil {
		t.Fatalf("Failed to unmarshal updated content: %v", err)
	}

	// Check if the SQLite module was added correctly
	modules, ok := updatedConfig["module"].(map[string]interface{})
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

	installer := &Installer{
		logger: logger,
		drush:  drush,
		fs:     fs,
	}

	drush.On("GetConfigSyncDir", mock.Anything, "/tmp", "default", false).Return("/tmp", nil)

	// Call the function to add the SQLite module
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

	// Read the updated file content
	updatedContent, err := afero.ReadFile(fs, tempFile.Name())
	if err != nil {
		t.Fatalf("Failed to read updated content from temp file: %v", err)
	}

	if string(updatedContent) != expectedContent {
		t.Fatalf("Expected content: %s, got: %s", expectedContent, updatedContent)
	}

	drush.AssertExpectations(t)
}
