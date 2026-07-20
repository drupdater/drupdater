package drupal

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/afero"
	"gopkg.in/yaml.v3"

	"go.uber.org/zap"
)

type Drush interface {
	InstallSite(ctx context.Context, path string, site string) error
	GetConfigSyncDir(ctx context.Context, path string, site string, create bool) (string, error)
}

type Composer interface {
	GetConfig(ctx context.Context, path string, key string) (string, error)
}

type Installer struct {
	logger   *zap.Logger
	drush    Drush
	composer Composer
	fs       afero.Fs
}

func NewInstaller(logger *zap.Logger, drush Drush, composer Composer) *Installer {
	return &Installer{
		logger:   logger,
		drush:    drush,
		composer: composer,
		fs:       afero.NewOsFs(),
	}
}

func (is *Installer) Install(ctx context.Context, path string, site string) error {

	is.logger.Info("installing site", zap.String("site", site))

	if err := is.ConfigureDatabase(ctx, path, site); err != nil {
		return err
	}

	if err := is.RemoveProfile(ctx, path, site); err != nil {
		return err
	}

	if err := is.drush.InstallSite(ctx, path, site); err != nil {
		return err
	}

	return nil
}

func (is *Installer) ConfigureDatabase(ctx context.Context, dir string, site string) error {

	siteLogger := is.logger.With(zap.String("site", site))
	siteLogger.Debug("configuring database", zap.String("dir", dir))

	webroot, err := is.composer.GetConfig(ctx, dir, "extra.drupal-scaffold.locations.web-root")
	if err != nil {
		return fmt.Errorf("failed to get Drupal web dir: %w", err)
	}
	webroot = strings.TrimSuffix(webroot, "/")

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

	isSqliteEnabled, _ := is.isSqliteModuleEnabled(ctx, dir, site)
	if !isSqliteEnabled {
		siteLogger.Debug("enabling sqlite module")
		if err := is.addSqliteModule(ctx, dir, site); err != nil {
			return fmt.Errorf("failed to enable sqlite module: %w", err)
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

	// Append the database and file settings to the site's existing settings.php. The file is
	// expected to already exist (every installed Drupal site has one); a missing file is an
	// error rather than something to create, since our snippet alone is not a valid settings.php.
	f, err := is.fs.OpenFile(settingsPath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open settings file: %w", err)
	}
	if _, err := f.Write([]byte(settings)); err != nil {
		f.Close() // ignore error; Write error takes precedence
		return fmt.Errorf("failed to write settings: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("failed to close settings file: %w", err)
	}

	return nil
}

func (is *Installer) isSqliteModuleEnabled(ctx context.Context, dir string, site string) (bool, error) {
	siteLogger := is.logger.With(zap.String("site", site))

	configSyncDir, err := is.drush.GetConfigSyncDir(ctx, dir, site, false)
	if err != nil {
		return false, err
	}
	coreExtensionPath := configSyncDir + "/core.extension.yml"
	siteLogger.Debug("checking if sqlite module is enabled", zap.String("path", coreExtensionPath))

	// Read the existing YAML file
	file, err := afero.ReadFile(is.fs, coreExtensionPath)
	if err != nil {
		return false, fmt.Errorf("failed to read core extension file: %w", err)
	}

	// Unmarshal the YAML data
	var config map[string]any
	if err := yaml.Unmarshal(file, &config); err != nil {
		return false, fmt.Errorf("failed to unmarshal core extension file: %w", err)
	}

	// Check if the sqlite module is enabled
	modules, ok := config["module"].(map[string]any)
	if !ok {
		return false, fmt.Errorf("core extension file %s has no module section", coreExtensionPath)
	}
	if enabled, exists := modules["sqlite"]; exists && enabled == 0 {
		siteLogger.Debug("sqlite module is enabled")
		return true, nil
	}

	siteLogger.Debug("sqlite module is not enabled")
	return false, nil
}

func (is *Installer) addSqliteModule(ctx context.Context, dir string, site string) error {

	siteLogger := is.logger.With(zap.String("site", site))

	configSyncDir, err := is.drush.GetConfigSyncDir(ctx, dir, site, false)
	if err != nil {
		return err
	}
	coreExtensionPath := configSyncDir + "/core.extension.yml"
	// Read the existing YAML file
	file, err := afero.ReadFile(is.fs, coreExtensionPath)
	if err != nil {
		return fmt.Errorf("failed to read core extension file: %w", err)
	}

	// Unmarshal the YAML data
	var config map[string]any
	if err := yaml.Unmarshal(file, &config); err != nil {
		return fmt.Errorf("failed to unmarshal core extension file: %w", err)
	}

	modules, ok := config["module"].(map[string]any)
	if !ok {
		return fmt.Errorf("core extension file %s has no module section", coreExtensionPath)
	}
	modules["sqlite"] = 0

	// Marshal the updated config back to YAML
	updatedConfig, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal updated config: %w", err)
	}

	// Write the updated config back to the file
	if err := afero.WriteFile(is.fs, coreExtensionPath, updatedConfig, 0644); err != nil {
		return fmt.Errorf("failed to write updated core extension file: %w", err)
	}
	siteLogger.Debug("sqlite module added to core extension file")

	return nil
}

func (is *Installer) RemoveProfile(ctx context.Context, dir string, site string) error {
	siteLogger := is.logger.With(zap.String("site", site))

	configSyncDir, err := is.drush.GetConfigSyncDir(ctx, dir, site, false)
	if err != nil {
		return err
	}
	coreExtensionPath := configSyncDir + "/core.extension.yml"

	// Open the file for reading
	fileToRead, err := is.fs.Open(coreExtensionPath)
	if err != nil {
		siteLogger.Error("failed to open file", zap.Error(err))
		return err
	}
	defer fileToRead.Close()

	profiles := []string{"standard"}

	// Read all lines into a slice, excluding both the "profile: <name>" key and the profile's
	// own entry in the module list (profiles are listed under module: with a high weight). A
	// line is dropped if it matches any configured profile; it is kept only when it matches
	// none — checking that once per line (not once per profile) so extra profiles can't
	// duplicate the kept lines.
	var lines []string
	scanner := bufio.NewScanner(fileToRead)
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		keep := true
		for _, profile := range profiles {
			if trimmed == "profile: "+profile || strings.HasPrefix(trimmed, profile+":") {
				keep = false
				break
			}
		}
		if keep {
			lines = append(lines, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	// Rewrite the file without the target line
	file, err := is.fs.Create(coreExtensionPath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	for _, line := range lines {
		_, err := writer.WriteString(line + "\n")
		if err != nil {
			return fmt.Errorf("failed to write file: %w", err)
		}
	}
	if err := writer.Flush(); err != nil {
		return fmt.Errorf("failed to flush file: %w", err)
	}

	return nil
}
