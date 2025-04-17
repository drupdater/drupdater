package services

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/drupdater/drupdater/internal/utils"

	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

type SettingsService interface {
	ConfigureDatabase(dir string, site string) error
	RemoveProfile(dir string, site string) error
}

type DrupalSettingsService struct {
	logger          *zap.Logger
	commandExecutor utils.CommandExecutor
}

func newDrupalSettingsService(logger *zap.Logger, commandExecutor utils.CommandExecutor) *DrupalSettingsService {
	return &DrupalSettingsService{
		logger:          logger,
		commandExecutor: commandExecutor,
	}
}

func (ss DrupalSettingsService) ConfigureDatabase(dir string, site string) error {

	siteLogger := ss.logger.With(zap.String("site", site))
	siteLogger.Debug("configuring database", zap.String("dir", dir))

	webroot, err := ss.commandExecutor.GetDrupalWebDir(dir)
	if err != nil {
		siteLogger.Error("failed to get Drupal web dir", zap.String("dir", dir), zap.Error(err))
		return err
	}

	sqliteFile, _ := filepath.Abs(fmt.Sprintf("%s/../%s.sqlite", dir, site))
	privatesDir, _ := filepath.Abs(fmt.Sprintf("%s/../private/%s", dir, site))

	settingsPath := dir + "/" + webroot + "/sites/" + site + "/settings.php"
	settings := `
$databases['default']['default'] = [
	'database' => '` + sqliteFile + `',
	'prefix' => '',
	'driver' => 'sqlite',
	'namespace' => 'Drupal\\sqlite\\Driver\\Database\\sqlite',
	'autoload' => 'core/modules/sqlite/src/Driver/Database/sqlite/',
];
$settings['skip_permissions_hardening'] = TRUE;
$settings['file_private_path'] = '` + privatesDir + `';
$settings['hash_salt'] = 'changeme';
`

	isSqliteEnabled, _ := ss.IsSqliteModuleEnabled(dir, site)
	if !isSqliteEnabled {
		siteLogger.Debug("enabling sqlite module")
		if err := ss.AddSqliteModule(dir, site); err != nil {
			siteLogger.Error("failed to enable sqlite module", zap.Error(err))
		}
		settings += `
if (isset($settings['config_exclude_modules'])) {
	$settings['config_exclude_modules'][] = 'sqlite';
} else {
	$settings['config_exclude_modules'] = ['sqlite'];
}
`
	}

	siteLogger.Debug("writing settings", zap.String("path", settingsPath), zap.String("settings", settings))

	// If the file doesn't exist, create it, or append to the file
	f, err := os.OpenFile(settingsPath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		siteLogger.Error("failed to open settings file", zap.Error(err))
		return err
	}
	if _, err := f.Write([]byte(settings)); err != nil {
		f.Close() // ignore error; Write error takes precedence
		siteLogger.Error("failed to write settings", zap.Error(err))
		return err
	}
	if err := f.Close(); err != nil {
		siteLogger.Error("failed to close settings file", zap.Error(err))
		return err
	}

	return nil
}

func (ss *DrupalSettingsService) IsSqliteModuleEnabled(dir string, site string) (bool, error) {

	siteLogger := ss.logger.With(zap.String("site", site))

	configSyncDir, err := ss.commandExecutor.GetConfigSyncDir(dir, site, false)
	if err != nil {
		return false, err
	}
	coreExtensionPath := configSyncDir + "/core.extension.yml"
	siteLogger.Debug("checking if sqlite module is enabled", zap.String("path", coreExtensionPath))

	// Read the existing YAML file
	file, err := os.ReadFile(coreExtensionPath)
	if err != nil {
		siteLogger.Error("failed to read core extension file", zap.Error(err))
		return false, err
	}

	// Unmarshal the YAML data
	var config map[string]interface{}
	if err := yaml.Unmarshal(file, &config); err != nil {
		siteLogger.Error("failed to unmarshal core extension file", zap.Error(err))
		return false, err
	}

	// Check if the sqlite module is enabled
	if enabled, exists := config["module"].(map[string]interface{})["sqlite"]; exists && enabled == 0 {
		siteLogger.Debug("sqlite module is enabled")
		return true, nil
	}

	siteLogger.Debug("sqlite module is not enabled")
	return false, nil
}

func (ss *DrupalSettingsService) AddSqliteModule(dir string, site string) error {

	siteLogger := ss.logger.With(zap.String("site", site))

	configSyncDir, err := ss.commandExecutor.GetConfigSyncDir(dir, site, false)
	if err != nil {
		return err
	}
	coreExtensionPath := configSyncDir + "/core.extension.yml"
	// Read the existing YAML file
	file, err := os.ReadFile(coreExtensionPath)
	if err != nil {
		siteLogger.Error("failed to read core extension file", zap.Error(err))
		return err
	}

	// Unmarshal the YAML data
	var config map[string]interface{}
	if err := yaml.Unmarshal(file, &config); err != nil {
		siteLogger.Error("failed to unmarshal core extension file", zap.Error(err))
		return err
	}

	config["module"].(map[string]interface{})["sqlite"] = 0

	// Marshal the updated config back to YAML
	updatedConfig, err := yaml.Marshal(config)
	if err != nil {
		siteLogger.Error("failed to marshal updated config", zap.Error(err))
		return err
	}

	// Write the updated config back to the file
	if err := os.WriteFile(coreExtensionPath, updatedConfig, 0644); err != nil {
		siteLogger.Error("failed to write updated core extension file", zap.Error(err))
		return err
	}
	siteLogger.Debug("sqlite module added to core extension file")

	return nil
}

func (ss *DrupalSettingsService) RemoveProfile(dir string, site string) error {

	siteLogger := ss.logger.With(zap.String("site", site))

	configSyncDir, err := ss.commandExecutor.GetConfigSyncDir(dir, site, false)
	if err != nil {
		return err
	}
	coreExtensionPath := configSyncDir + "/core.extension.yml"

	// Open the file for reading
	fileToRead, err := os.Open(coreExtensionPath)
	if err != nil {
		siteLogger.Error("Error opening file:", zap.Error(err))
		return err
	}
	defer fileToRead.Close()

	profiles := []string{"standard"}

	// Read all lines into a slice, excluding the target line
	var lines []string
	scanner := bufio.NewScanner(fileToRead)
	for scanner.Scan() {
		line := scanner.Text()

		for _, profile := range profiles {
			if strings.TrimSpace(line) != "profile: "+profile && !strings.Contains(line, profile+":") {
				lines = append(lines, line)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		siteLogger.Error("Error reading file:", zap.Error(err))
		return err
	}

	// Rewrite the file without the target line
	file, err := os.Create(coreExtensionPath)
	if err != nil {
		siteLogger.Error("Error creating file:", zap.Error(err))
		return err
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	for _, line := range lines {
		_, err := writer.WriteString(line + "\n")
		if err != nil {
			siteLogger.Error("Error writing to file:", zap.Error(err))
			return err
		}
	}
	if err := writer.Flush(); err != nil {
		fmt.Println("Error flushing to file:", err)
	}

	return nil
}
