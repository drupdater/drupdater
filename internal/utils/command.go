package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/maypok86/otter"
	"github.com/spf13/afero"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

// CommandExecutor interface for executing commands
type CommandExecutor interface {
	ExecDrush(ctx context.Context, dir string, site string, args ...string) (string, error)
	ExecComposer(ctx context.Context, dir string, args ...string) (string, error)
	InstallDependencies(ctx context.Context, dir string) error
	InstallSite(ctx context.Context, dir string, site string) error
	GetDrupalWebDir(ctx context.Context, dir string) (string, error)
	GetConfigSyncDir(ctx context.Context, dir string, site string, relative bool) (string, error)
	ExportConfiguration(ctx context.Context, dir string, site string) error
	UpdateSite(ctx context.Context, dir string, site string) error
	ConfigResave(ctx context.Context, dir string, site string) error
	UpdateDependencies(ctx context.Context, dir string, packagesToUpdate []string, packagesToKeep []string, minimalChanges bool, dryRun bool) (string, error)
	IsPackageInstalled(ctx context.Context, dir string, packageToCheck string) (bool, error)
	RunRector(ctx context.Context, dir string) (string, error)
	GenerateDiffTable(ctx context.Context, path string, targetBranch string, withLinks bool) (string, error)
	IsModuleEnabled(ctx context.Context, dir string, site string, module string) (bool, error)
	LocalizeTranslations(ctx context.Context, dir string, site string) error
	GetTranslationPath(ctx context.Context, dir string, site string, relative bool) (string, error)
	GetComposerConfig(ctx context.Context, dir string, key string) (string, error)
	SetComposerConfig(ctx context.Context, dir string, key string, value string) error
	UpdateComposerLockHash(ctx context.Context, dir string) error
	RunPHPCBF(ctx context.Context, dir string) error
	RunPHPCS(ctx context.Context, dir string) (string, error)
	InstallPackages(ctx context.Context, dir string, args ...string) (string, error)
	RemovePackages(ctx context.Context, dir string, packages ...string) (string, error)
	GetInstalledPackageVersion(ctx context.Context, dir string, packageName string) (string, error)
	GetCustomCodeDirectories(ctx context.Context, dir string) ([]string, error)
	GetComposerAllowPlugins(ctx context.Context, dir string) (map[string]bool, error)
	SetComposerAllowPlugins(ctx context.Context, dir string, plugins map[string]bool) error
	RunComposerNormalize(ctx context.Context, dir string) (string, error)
}

// DefaultCommandExecutor is the default implementation of CommandExecutor
type DefaultCommandExecutor struct {
	logger *zap.Logger
	cache  otter.Cache[string, string]
	fs     afero.Fs
}

func NewCommandExecutor(logger *zap.Logger, cache otter.Cache[string, string]) CommandExecutor {
	return DefaultCommandExecutor{
		logger: logger,
		cache:  cache,
		fs:     afero.NewOsFs(),
	}
}

var execCommand = exec.CommandContext

func (e DefaultCommandExecutor) ExecDrush(ctx context.Context, dir string, site string, args ...string) (string, error) {
	command := execCommand(ctx, "composer", append([]string{"exec", "--", "drush"}, args...)...)
	command.Dir = dir
	// os.Environ() preserves the current environment variables
	command.Env = append(command.Env, "SITE_NAME="+site)
	command.Env = append(command.Env, os.Environ()...)

	out, err := command.CombinedOutput()
	output := strings.TrimSuffix(string(out), "\n")

	e.logger.Debug("executing drush", zap.String("dir", dir), zap.String("site", site), zap.Strings("args", args), zap.String("output", output))

	return output, err
}

func (e DefaultCommandExecutor) ExecComposer(ctx context.Context, dir string, args ...string) (string, error) {
	command := execCommand(ctx, "composer", args...)
	command.Dir = dir

	out, err := command.CombinedOutput()
	output := strings.TrimSuffix(string(out), "\n")

	e.logger.Debug("executing composer", zap.String("dir", dir), zap.Strings("args", args), zap.String("output", output))

	// Create a child context with a cancellation signal
	_, childCancel := context.WithCancel(ctx)
	defer childCancel()

	if err != nil {

		childCancel() // Cancel the child context if there's an error
	}

	return output, err
}

func (e DefaultCommandExecutor) InstallDependencies(ctx context.Context, dir string) error {
	e.logger.Debug("installing dependencies")
	out, err := e.ExecComposer(ctx, dir, "install", "--no-interaction", "--no-progress", "--optimize-autoloader")
	if err != nil {
		return fmt.Errorf("failed to install dependencies: %w, output: %s", err, out)
	}
	return err
}

func (e DefaultCommandExecutor) InstallSite(ctx context.Context, dir string, site string) error {
	e.logger.Debug("installing site")
	_, err := e.ExecDrush(ctx, dir, site, "--existing-config", "--yes", "site:install", "--sites-subdir="+site)

	return err
}

func (e DefaultCommandExecutor) GetDrupalWebDir(ctx context.Context, dir string) (string, error) {
	cacheKey := fmt.Sprintf("web-dir_%s", dir)
	value, ok := e.cache.Get(cacheKey)
	if ok {
		return value, nil
	}

	value, err := e.ExecComposer(ctx, dir, "config", "extra.drupal-scaffold.locations.web-root")
	if err != nil {
		return "", err
	}
	value = strings.TrimSuffix(value, "/")
	e.cache.Set(cacheKey, value)
	return value, nil
}

func (e DefaultCommandExecutor) GetConfigSyncDir(ctx context.Context, dir string, site string, relative bool) (string, error) {
	cacheKey := fmt.Sprintf("config-sync-dir_%s_%s_%t", dir, site, relative)
	value, ok := e.cache.Get(cacheKey)
	if ok {
		return value, nil
	}
	configSyncDir, err := e.ExecDrush(ctx, dir, site, "ev", "print realpath(\\Drupal\\Core\\Site\\Settings::get('config_sync_directory'))")
	if err != nil {
		return "", err
	}
	if relative {
		configSyncDir = strings.TrimLeft(strings.TrimPrefix(configSyncDir, dir), "/")
	}
	e.cache.Set(cacheKey, configSyncDir)
	return configSyncDir, nil
}

func (e DefaultCommandExecutor) ExportConfiguration(ctx context.Context, dir string, site string) error {
	e.logger.Debug("exporting configuration")
	_, err := e.ExecDrush(ctx, dir, site, "config:export", "--yes")
	return err
}

func (e DefaultCommandExecutor) UpdateSite(ctx context.Context, dir string, site string) error {
	e.logger.Debug("updating site")
	_, err := e.ExecDrush(ctx, dir, site, "updatedb", "--yes", "-vvv")
	return err
}

func (e DefaultCommandExecutor) ConfigResave(ctx context.Context, dir string, site string) error {
	e.logger.Debug("config resave")
	_, err := e.ExecDrush(ctx, dir, site, "php:script", "/opt/drupdater/config-resave.php")
	return err
}

func (e DefaultCommandExecutor) UpdateDependencies(ctx context.Context, dir string, packagesToUpdate []string, packagesToKeep []string, minimalChanges bool, dryRun bool) (string, error) {
	e.logger.Debug("updating dependencies", zap.Strings("packagesToUpdate", packagesToUpdate))
	args := append([]string{"update", "--no-interaction", "--no-progress", "--optimize-autoloader", "--with-all-dependencies", "--no-ansi"}, packagesToUpdate...)
	for _, packageToKeep := range packagesToKeep {
		args = append(args, fmt.Sprintf("--with=%s", packageToKeep))
	}
	if minimalChanges {
		args = append(args, "--minimal-changes")
	}
	if dryRun {
		args = append(args, "--dry-run")
	} else {
		args = append(args, "--bump-after-update")
	}
	return e.ExecComposer(ctx, dir, args...)
}

func (e DefaultCommandExecutor) GetComposerAllowPlugins(ctx context.Context, dir string) (map[string]bool, error) {
	allowPluginsJSON, err := e.GetComposerConfig(ctx, dir, "allow-plugins")
	if err != nil {
		e.logger.Error("failed to set composer config", zap.Error(err))
	}

	var allowPlugins map[string]bool

	err = json.Unmarshal([]byte(allowPluginsJSON), &allowPlugins)
	if err != nil {
		return nil, err
	}

	return allowPlugins, nil
}

func (e DefaultCommandExecutor) SetComposerAllowPlugins(ctx context.Context, dir string, plugins map[string]bool) error {
	for key, value := range plugins {
		err := e.SetComposerConfig(ctx, dir, "allow-plugins."+key, fmt.Sprintf("%t", value))
		if err != nil {
			return err
		}
	}
	return nil
}

func (e DefaultCommandExecutor) IsPackageInstalled(ctx context.Context, dir string, packageToCheck string) (bool, error) {
	_, err := e.ExecComposer(ctx, dir, "show", "--locked", packageToCheck)
	if err != nil {
		return false, err
	}
	return true, nil
}

func (e DefaultCommandExecutor) InstallPackages(ctx context.Context, dir string, args ...string) (string, error) {
	return e.ExecComposer(ctx, dir, append([]string{"require"}, args...)...)
}

func (e DefaultCommandExecutor) RemovePackages(ctx context.Context, dir string, packages ...string) (string, error) {
	return e.ExecComposer(ctx, dir, append([]string{"remove"}, packages...)...)
}

func (e DefaultCommandExecutor) RunRector(ctx context.Context, dir string) (string, error) {
	e.logger.Debug("remove deprecations")

	customCodeDirectories, err := e.GetCustomCodeDirectories(ctx, dir)
	if err != nil {
		return "", err
	}
	if len(customCodeDirectories) == 0 {
		e.logger.Debug("no custom code directories found")
		return `{
    "totals": {
        "changed_files": 0,
        "errors": 0
    },
    "file_diffs": [],
    "changed_files": []
}`, nil

	}

	args := []string{"exec", "--", "rector", "process", "--config=/opt/drupdater/rector.php", "--no-progress-bar", "--no-diffs", "--debug", "--output-format=json"}
	args = append(args, customCodeDirectories...)

	return e.ExecComposer(ctx, dir, args...)
}

func (e DefaultCommandExecutor) GenerateDiffTable(ctx context.Context, dir string, targetBranch string, withLinks bool) (string, error) {
	e.logger.Debug("generating diff table")
	args := []string{"diff", targetBranch}
	if withLinks {
		args = append(args, "--with-links")
	}

	return e.ExecComposer(ctx, dir, args...)
}

func (e DefaultCommandExecutor) IsModuleEnabled(ctx context.Context, dir string, site string, module string) (bool, error) {
	e.logger.Debug("checking if module is enabled")
	out, err := e.ExecDrush(ctx, dir, site, "pm:list", "--status=enabled", "--field=name", "--filter="+module)
	return out == module, err
}

func (e DefaultCommandExecutor) LocalizeTranslations(ctx context.Context, dir string, site string) error {
	e.logger.Debug("localizing translations")
	_, err := e.ExecDrush(ctx, dir, site, "locale-deploy:localize-translations")
	return err
}

func (e DefaultCommandExecutor) GetTranslationPath(ctx context.Context, dir string, site string, relative bool) (string, error) {
	cacheKey := fmt.Sprintf("translation-path_%s_%s_%t", dir, site, relative)
	value, ok := e.cache.Get(cacheKey)
	if ok {
		return value, nil
	}
	translationPath, err := e.ExecDrush(ctx, dir, site, "ev", "print realpath(\\Drupal::config('locale.settings')->get('translation.path'))")
	if err != nil {
		return "", err
	}

	if relative {
		translationPath = strings.TrimLeft(strings.TrimPrefix(translationPath, dir), "/")
	}

	e.cache.Set(cacheKey, translationPath)
	return translationPath, nil
}

func (e DefaultCommandExecutor) GetComposerConfig(ctx context.Context, dir string, key string) (string, error) {
	e.logger.Debug("getting composer config", zap.String("key", key))
	//ctx = context.Background()
	return e.ExecComposer(ctx, dir, "config", "--json", key)
}

func (e DefaultCommandExecutor) SetComposerConfig(ctx context.Context, dir string, key string, value string) error {
	e.logger.Debug("setting composer config", zap.String("key", key), zap.String("value", value))
	_, err := e.ExecComposer(ctx, dir, "config", "--json", key, value)
	return err
}

func (e DefaultCommandExecutor) GetInstalledPackageVersion(ctx context.Context, dir string, packageName string) (string, error) {
	e.logger.Debug("getting installed package version")
	out, err := e.ExecComposer(ctx, dir, "show", packageName, "--locked", "--no-ansi", "--format=json")
	if err != nil {
		return "", err
	}

	var composerShow struct {
		Versions []string `json:"versions"`
	}

	if err := json.Unmarshal([]byte(out), &composerShow); err != nil {
		return "", err
	}

	return composerShow.Versions[0], nil
}

func (e DefaultCommandExecutor) UpdateComposerLockHash(ctx context.Context, dir string) error {
	e.logger.Debug("updating composer lock hash")
	_, err := e.ExecComposer(ctx, dir, "update", "--lock", "--no-install")
	return err
}

func (e DefaultCommandExecutor) RunPHPCS(ctx context.Context, dir string) (string, error) {
	e.logger.Debug("running phpcs")
	return e.ExecComposer(ctx, dir, "exec", "--", "phpcs", "--report=json", "-q", "--runtime-set", "ignore_errors_on_exit", "1", "--runtime-set", "ignore_warnings_on_exit", "1")
}

func (e DefaultCommandExecutor) RunComposerNormalize(ctx context.Context, dir string) (string, error) {
	e.logger.Debug("running composer normalize")
	return e.ExecComposer(ctx, dir, "normalize")
}

func (e DefaultCommandExecutor) RunPHPCBF(ctx context.Context, dir string) error {
	e.logger.Debug("running phpcbf")
	_, err := e.ExecComposer(ctx, dir, "exec", "--", "phpcbf")
	return err
}

func (e DefaultCommandExecutor) GetCustomCodeDirectories(ctx context.Context, dir string) ([]string, error) {
	webDir, err := e.GetDrupalWebDir(ctx, dir)
	if err != nil {
		return nil, err
	}

	possibleDirectories := []string{webDir + "/modules/custom", webDir + "/themes/custom", webDir + "/profiles/custom"}
	var customCodeDirectories []string
	for _, possibleDirectory := range possibleDirectories {
		if _, err := e.fs.Stat(dir + "/" + possibleDirectory); os.IsNotExist(err) {
			continue
		}
		customCodeDirectories = append(customCodeDirectories, possibleDirectory)
	}
	return customCodeDirectories, nil
}

var Module = fx.Provide(
	NewCommandExecutor,
)
