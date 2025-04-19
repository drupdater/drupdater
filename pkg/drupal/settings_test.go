package drupal

import (
	"testing"

	"github.com/drupdater/drupdater/pkg/drush"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/mock"

	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

func TestIsSqliteModuleEnabled(t *testing.T) {
	logger := zap.NewNop()
	drush := drush.NewMockRunner(t)

	fs := afero.NewMemMapFs()

	settingsService := &DefaultSettingsService{
		logger: logger,
		drush:  drush,
		fs:     fs,
	}

	dir := "/tmp"
	site := "default"
	configSyncDir := "/tmp/config/sync"
	coreExtensionPath := configSyncDir + "/core.extension.yml"

	drush.On("GetConfigSyncDir", mock.Anything, "/tmp", "default", false).Return(configSyncDir, nil)

	// Create a temporary directory and file to act as the config sync directory and core.extension.yml
	if err := fs.MkdirAll(configSyncDir, 0755); err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer fs.RemoveAll(configSyncDir)

	// Write initial YAML content to the temp file
	initialContent := `
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
	if err := afero.WriteFile(fs, coreExtensionPath, []byte(initialContent), 0644); err != nil {
		t.Fatalf("Failed to write initial content to core.extension.yml: %v", err)
	}

	enabled, err := settingsService.isSqliteModuleEnabled(t.Context(), dir, site)
	if err != nil {
		t.Fatalf("Failed to check if sqlite module is enabled: %v", err)
	}

	if !enabled {
		t.Fatalf("Expected sqlite module to be enabled, but it was not")
	}

	// Modify the YAML content to disable the sqlite module
	disabledContent := `
module:
  existing_module: 0
`
	if err := afero.WriteFile(fs, coreExtensionPath, []byte(disabledContent), 0644); err != nil {
		t.Fatalf("Failed to write disabled content to core.extension.yml: %v", err)
	}

	enabled, err = settingsService.isSqliteModuleEnabled(t.Context(), dir, site)
	if err != nil {
		t.Fatalf("Failed to check if sqlite module is enabled: %v", err)
	}

	if enabled {
		t.Fatalf("Expected sqlite module to be disabled, but it was enabled")
	}

	drush.AssertExpectations(t)
}

func TestAddSqliteModule(t *testing.T) {

	logger := zap.NewNop()
	drush := drush.NewMockRunner(t)
	fs := afero.NewMemMapFs()

	// Create a temporary file to act as the core.extension.yml
	tempFile, err := fs.Create("/tmp/core.extension.yml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer fs.Remove(tempFile.Name())

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

	settingsService := &DefaultSettingsService{
		logger: logger,
		drush:  drush,
		fs:     fs,
	}

	drush.On("GetConfigSyncDir", mock.Anything, "/tmp", "default", false).Return("/tmp", nil)

	// Call the function to add the SQLite module
	if err := settingsService.addSqliteModule(t.Context(), "/tmp", "default"); err != nil {
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
	drush := drush.NewMockRunner(t)
	fs := afero.NewMemMapFs()

	// Create a temporary file to act as the core.extension.yml
	tempFile, err := fs.Create("/tmp/core.extension.yml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer fs.Remove(tempFile.Name())

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

	settingsService := &DefaultSettingsService{
		logger: logger,
		drush:  drush,
		fs:     fs,
	}

	drush.On("GetConfigSyncDir", mock.Anything, "/tmp", "default", false).Return("/tmp", nil)

	// Call the function to add the SQLite module
	if err := settingsService.RemoveProfile(t.Context(), "/tmp", "default"); err != nil {
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
