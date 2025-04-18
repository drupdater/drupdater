package utils

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"unicode/utf8"

	"github.com/maypok86/otter"
	"github.com/spf13/afero"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

// CommandExecutor interface for executing commands
type CommandExecutor interface {
	ExecDrush(ctx context.Context, dir string, site string, args ...string) (string, error)
	ExecComposer(ctx context.Context, dir string, args ...string) (string, error)
	InstallSite(ctx context.Context, dir string, site string) error
	GetDrupalWebDir(ctx context.Context, dir string) (string, error)
	GetConfigSyncDir(ctx context.Context, dir string, site string, relative bool) (string, error)
	ExportConfiguration(ctx context.Context, dir string, site string) error
	UpdateSite(ctx context.Context, dir string, site string) error
	ConfigResave(ctx context.Context, dir string, site string) error
	RunRector(ctx context.Context, dir string) (string, error)
	GenerateDiffTable(ctx context.Context, path string, targetBranch string, withLinks bool) (string, error)
	IsModuleEnabled(ctx context.Context, dir string, site string, module string) (bool, error)
	LocalizeTranslations(ctx context.Context, dir string, site string) error
	GetTranslationPath(ctx context.Context, dir string, site string, relative bool) (string, error)
	UpdateComposerLockHash(ctx context.Context, dir string) error
	RunPHPCBF(ctx context.Context, dir string) error
	RunPHPCS(ctx context.Context, dir string) (string, error)
	GetCustomCodeDirectories(ctx context.Context, dir string) ([]string, error)
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

	return output, err
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

	out, err := e.ExecComposer(ctx, dir, args...)
	if err != nil {
		return "", err
	}

	if withLinks {
		// If table is too long, Github/Gitlab will not accept it. So we use the version without the links.
		tableCharCount := utf8.RuneCountInString(out)
		if tableCharCount > 63000 {
			return e.GenerateDiffTable(ctx, dir, targetBranch, false)
		}
	}

	return out, err
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

func (e DefaultCommandExecutor) UpdateComposerLockHash(ctx context.Context, dir string) error {
	e.logger.Debug("updating composer lock hash")
	_, err := e.ExecComposer(ctx, dir, "update", "--lock", "--no-install")
	return err
}

func (e DefaultCommandExecutor) RunPHPCS(ctx context.Context, dir string) (string, error) {
	e.logger.Debug("running phpcs")
	return e.ExecComposer(ctx, dir, "exec", "--", "phpcs", "--report=json", "-q", "--runtime-set", "ignore_errors_on_exit", "1", "--runtime-set", "ignore_warnings_on_exit", "1")
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
