package drupal

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/drupdater/drupdater/pkg/composer"
	"github.com/spf13/afero"
	"gopkg.in/yaml.v3"

	"go.uber.org/zap"
)

type Drush interface {
	InstallSite(ctx context.Context, path string, site string) error
	// GetConfigSyncDir returns the site's absolute config sync directory, before it is installed too.
	GetConfigSyncDir(ctx context.Context, path string, site string) (string, error)
}

type Composer interface {
	GetConfig(ctx context.Context, path string, key string) (string, error)
}

type Installer struct {
	logger   *zap.Logger
	drush    Drush
	composer Composer
	fs       afero.Fs
	dataDir  string

	// blocks holds the settings appended per settings.php, so a reconfigure after RestoreSettings
	// reuses the first decision: by then core.extension.yml already carries our sqlite entry.
	mu     sync.Mutex
	blocks map[string]string
	// syncDirs saves a drush bootstrap per lookup: settings.php fixes the directory for the run.
	syncDirs map[string]string
}

// NewInstaller writes each site's SQLite database and private files under dataDir, which the
// caller owns and removes.
func NewInstaller(logger *zap.Logger, drush Drush, composer Composer, dataDir string) *Installer {
	return &Installer{
		logger:   logger,
		drush:    drush,
		composer: composer,
		fs:       afero.NewOsFs(),
		dataDir:  dataDir,
	}
}

// Install leaves settings.php as it found it; only the database under dataDir remains.
func (is *Installer) Install(ctx context.Context, path string, site string) (err error) {

	is.logger.Info("installing site", zap.String("site", site))

	settingsPath, err := is.configureDatabase(ctx, path, site)
	if err != nil {
		return err
	}
	defer func() {
		if restoreErr := is.restoreSettingsFile(site, settingsPath); restoreErr != nil {
			err = errors.Join(err, restoreErr)
		}
	}()

	if err := is.RemoveProfile(ctx, path, site); err != nil {
		return err
	}

	if err := is.drush.InstallSite(ctx, path, site); err != nil {
		return err
	}

	return nil
}

// settingsMarker opens the appended block, so RestoreSettings can cut it off again — including
// one a killed earlier run left behind.
const settingsMarker = "// Added by drupdater: test database settings."

func (is *Installer) settingsPath(ctx context.Context, dir string, site string) (string, error) {
	webroot, err := composer.WebRoot(ctx, is.composer, dir)
	if err != nil {
		return "", fmt.Errorf("failed to get Drupal web dir: %w", err)
	}
	return dir + "/" + webroot + "/sites/" + site + "/settings.php", nil
}

// ConfigureDatabase points the site at a throwaway SQLite database. Pair it with RestoreSettings:
// settings.php is tracked, and nothing may stage it while the block is in place.
func (is *Installer) ConfigureDatabase(ctx context.Context, dir string, site string) error {
	_, err := is.configureDatabase(ctx, dir, site)
	return err
}

func (is *Installer) configureDatabase(ctx context.Context, dir string, site string) (string, error) {

	siteLogger := is.logger.With(zap.String("site", site))
	siteLogger.Debug("configuring database", zap.String("dir", dir))

	settingsPath, err := is.settingsPath(ctx, dir, site)
	if err != nil {
		return "", err
	}

	// Checked on every call: core.extension.yml comes back from git without the sqlite entry
	// whenever a run is restored, and Drupal refuses to install without it ("SQLite is
	// providing the database driver").
	isSqliteEnabled, _ := is.isSqliteModuleEnabled(ctx, dir, site)
	if !isSqliteEnabled {
		siteLogger.Debug("enabling sqlite module")
		if err := is.addSqliteModule(ctx, dir, site); err != nil {
			return "", fmt.Errorf("failed to enable sqlite module: %w", err)
		}
	}

	is.mu.Lock()
	if is.blocks == nil {
		is.blocks = map[string]string{}
	}
	settings, seen := is.blocks[settingsPath]
	if !seen {
		settings = is.settingsBlock(site, isSqliteEnabled)
		is.blocks[settingsPath] = settings
	}
	is.mu.Unlock()

	// Never created: this snippet alone is not a valid settings.php.
	original, err := afero.ReadFile(is.fs, settingsPath)
	if err != nil {
		return "", fmt.Errorf("failed to open settings file: %w", err)
	}

	siteLogger.Debug("writing settings", zap.String("path", settingsPath), zap.String("settings", settings))

	content := append(stripSettingsBlock(original), settings...)
	if err := is.writeSettings(settingsPath, content); err != nil {
		return "", fmt.Errorf("failed to write settings: %w", err)
	}

	return settingsPath, nil
}

func (is *Installer) settingsBlock(site string, isSqliteEnabled bool) string {
	sqliteFile := filepath.Join(is.dataDir, site+".sqlite")
	privatesDir := filepath.Join(is.dataDir, "private", site)

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

	// Only when the project does not use sqlite itself, or the export would drop a real module.
	if !isSqliteEnabled {
		settings += `
if (isset($settings['config_exclude_modules'])) {
	$settings['config_exclude_modules'][] = 'sqlite';
} else {
	$settings['config_exclude_modules'] = ['sqlite'];
}
`
	}
	return settings
}

// RestoreSettings removes the block ConfigureDatabase appended, leaving settings.php byte-for-byte
// as the project has it. A file without the block is left alone.
func (is *Installer) RestoreSettings(ctx context.Context, dir string, site string) error {
	settingsPath, err := is.settingsPath(ctx, dir, site)
	if err != nil {
		return err
	}
	return is.restoreSettingsFile(site, settingsPath)
}

func (is *Installer) restoreSettingsFile(site string, settingsPath string) error {
	content, err := afero.ReadFile(is.fs, settingsPath)
	if err != nil {
		return fmt.Errorf("failed to read settings file: %w", err)
	}
	stripped := stripSettingsBlock(content)
	if len(stripped) == len(content) {
		return nil
	}

	is.logger.Debug("restoring settings", zap.String("site", site), zap.String("path", settingsPath))
	if err := is.writeSettings(settingsPath, stripped); err != nil {
		return fmt.Errorf("failed to restore settings: %w", err)
	}
	return nil
}

// stripSettingsBlock cuts content at the marker, together with the newline the block starts with.
func stripSettingsBlock(content []byte) []byte {
	i := bytes.Index(content, []byte(settingsMarker))
	if i < 0 {
		return content
	}
	if i > 0 && content[i-1] == '\n' {
		i--
	}
	return content[:i:i]
}

// writeSettings truncates in place rather than replacing the file, keeping its mode and owner.
func (is *Installer) writeSettings(path string, content []byte) error {
	// Equivalent mutant: without O_CREATE the mode is never used.
	// mutator-disable-next-line numbers/incrementer
	f, err := is.fs.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0)
	if err != nil {
		return err
	}
	if _, err := f.Write(content); err != nil {
		// Equivalent mutant: best effort, the Write error takes precedence.
		// mutator-disable-next-line statement/remove
		f.Close()
		return err
	}
	return f.Close()
}

// coreExtensionPath returns the site's core.extension.yml.
func (is *Installer) coreExtensionPath(ctx context.Context, dir string, site string) (string, error) {
	key := dir + "\x00" + site
	is.mu.Lock()
	configSyncDir, ok := is.syncDirs[key]
	is.mu.Unlock()
	if !ok {
		var err error
		if configSyncDir, err = is.drush.GetConfigSyncDir(ctx, dir, site); err != nil {
			return "", err
		}
		is.mu.Lock()
		if is.syncDirs == nil {
			is.syncDirs = map[string]string{}
		}
		is.syncDirs[key] = configSyncDir
		is.mu.Unlock()
	}
	return configSyncDir + "/core.extension.yml", nil
}

// readCoreExtension decodes core.extension.yml. A document with no module section is an error:
// every caller here needs one.
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

	// Matched once per line, not once per profile, or extra profiles duplicate the kept lines.
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
