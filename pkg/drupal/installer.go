package drupal

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/drupdater/drupdater/pkg/composer"
	"github.com/spf13/afero"
	"gopkg.in/yaml.v3"

	"go.uber.org/zap"
)

type Drush interface {
	InstallSite(ctx context.Context, path string, site string) error
	// GetConfigSyncDir returns the site's config sync directory, relative to path or absolute.
	GetConfigSyncDir(ctx context.Context, path string, site string, relative bool) (string, error)
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

// settingsMarker tags the block ConfigureDatabase appends to settings.php. Each site is
// configured twice against the same file — baseline, then before the update hooks — so the
// marker keeps the append idempotent instead of stacking duplicate settings.
const settingsMarker = "// Added by drupdater: test database settings."

func (is *Installer) ConfigureDatabase(ctx context.Context, dir string, site string) error {

	siteLogger := is.logger.With(zap.String("site", site))
	siteLogger.Debug("configuring database", zap.String("dir", dir))

	webroot, err := composer.WebRoot(ctx, is.composer, dir)
	if err != nil {
		return fmt.Errorf("failed to get Drupal web dir: %w", err)
	}

	settingsPath := dir + "/" + webroot + "/sites/" + site + "/settings.php"

	// Ensured on every call, ahead of the marker check below, because the two states are
	// independent. settings.php survives a run -- it is deliberately never committed -- while
	// core.extension.yml comes back from git whenever the working copy is reused, and the
	// configuration export writes it without sqlite (config_exclude_modules sees to that). A
	// second run in the same directory therefore finds the marker set and the module entry gone,
	// and Drupal refuses to install: "Unable to uninstall the SQLite module because: The module
	// 'SQLite' is providing the database driver 'sqlite'".
	isSqliteEnabled, _ := is.isSqliteModuleEnabled(ctx, dir, site)
	if !isSqliteEnabled {
		siteLogger.Debug("enabling sqlite module")
		if err := is.addSqliteModule(ctx, dir, site); err != nil {
			return fmt.Errorf("failed to enable sqlite module: %w", err)
		}
	}

	// The marker guards only the append: without it a repeated call stacks a second $databases
	// block onto the file. config_exclude_modules is part of that append, so it is already there
	// from the first call.
	if existing, err := afero.ReadFile(is.fs, settingsPath); err == nil && strings.Contains(string(existing), settingsMarker) {
		siteLogger.Debug("settings already configured, skipping", zap.String("path", settingsPath))
		return nil
	}

	sqliteFile, _ := filepath.Abs(fmt.Sprintf("%s/../%s.sqlite", dir, site))
	privatesDir, _ := filepath.Abs(fmt.Sprintf("%s/../private/%s", dir, site))

	settings := `
` + settingsMarker + `
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

	if !isSqliteEnabled {
		settings += `
if (isset($settings['config_exclude_modules'])) {
	$settings['config_exclude_modules'][] = 'sqlite';
} else {
	$settings['config_exclude_modules'] = ['sqlite'];
}
`
	}

	siteLogger.Debug("writing settings", zap.String("path", settingsPath), zap.String("settings", settings))

	// A missing settings.php is an error, not something to create: this snippet alone is not a
	// valid one, and every installed site already has the file.
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

// coreExtensionPath returns the site's core.extension.yml, the file that records which modules
// and profile the site was installed with.
func (is *Installer) coreExtensionPath(ctx context.Context, dir string, site string) (string, error) {
	configSyncDir, err := is.drush.GetConfigSyncDir(ctx, dir, site, false)
	if err != nil {
		return "", err
	}
	return configSyncDir + "/core.extension.yml", nil
}

// readCoreExtension reads and decodes core.extension.yml, returning its path alongside the whole
// document and its module section — a file without one is unusable to every caller here.
func (is *Installer) readCoreExtension(ctx context.Context, dir string, site string) (path string, config map[string]any, modules map[string]any, err error) {
	path, err = is.coreExtensionPath(ctx, dir, site)
	if err != nil {
		return "", nil, nil, err
	}

	file, err := afero.ReadFile(is.fs, path)
	if err != nil {
		return path, nil, nil, fmt.Errorf("failed to read core extension file: %w", err)
	}

	if err := yaml.Unmarshal(file, &config); err != nil {
		return path, nil, nil, fmt.Errorf("failed to unmarshal core extension file: %w", err)
	}

	modules, ok := config["module"].(map[string]any)
	if !ok {
		return path, config, nil, fmt.Errorf("core extension file %s has no module section", path)
	}
	return path, config, modules, nil
}

func (is *Installer) isSqliteModuleEnabled(ctx context.Context, dir string, site string) (bool, error) {
	siteLogger := is.logger.With(zap.String("site", site))

	coreExtensionPath, _, modules, err := is.readCoreExtension(ctx, dir, site)
	if err != nil {
		return false, err
	}
	siteLogger.Debug("checking if sqlite module is enabled", zap.String("path", coreExtensionPath))

	if enabled, exists := modules["sqlite"]; exists && enabled == 0 {
		siteLogger.Debug("sqlite module is enabled")
		return true, nil
	}

	siteLogger.Debug("sqlite module is not enabled")
	return false, nil
}

func (is *Installer) addSqliteModule(ctx context.Context, dir string, site string) error {

	siteLogger := is.logger.With(zap.String("site", site))

	coreExtensionPath, config, modules, err := is.readCoreExtension(ctx, dir, site)
	if err != nil {
		return err
	}
	modules["sqlite"] = 0

	updatedConfig, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal updated config: %w", err)
	}

	if err := afero.WriteFile(is.fs, coreExtensionPath, updatedConfig, 0644); err != nil {
		return fmt.Errorf("failed to write updated core extension file: %w", err)
	}
	siteLogger.Debug("sqlite module added to core extension file")

	return nil
}

func (is *Installer) RemoveProfile(ctx context.Context, dir string, site string) error {
	siteLogger := is.logger.With(zap.String("site", site))

	coreExtensionPath, err := is.coreExtensionPath(ctx, dir, site)
	if err != nil {
		return err
	}

	fileToRead, err := is.fs.Open(coreExtensionPath)
	if err != nil {
		siteLogger.Error("failed to open file", zap.Error(err))
		return err
	}
	defer fileToRead.Close()

	profiles := []string{"standard"}

	// Drop both the "profile: <name>" key and the profile's own entry under module:. Matched
	// once per line rather than once per profile, or extra profiles would duplicate kept lines.
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
