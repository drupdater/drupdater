package drush

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/maypok86/otter"
	"github.com/spf13/afero"
	"go.uber.org/zap"
)

var execCommand = exec.CommandContext

// Runner interface for executing commands
type Runner interface {
	ExecDrush(ctx context.Context, dir string, site string, args ...string) (string, error)
	InstallSite(ctx context.Context, dir string, site string) error
	GetConfigSyncDir(ctx context.Context, dir string, site string, relative bool) (string, error)
	ExportConfiguration(ctx context.Context, dir string, site string) error
	UpdateSite(ctx context.Context, dir string, site string) error
	ConfigResave(ctx context.Context, dir string, site string) error
	IsModuleEnabled(ctx context.Context, dir string, site string, module string) (bool, error)
	LocalizeTranslations(ctx context.Context, dir string, site string) error
	GetTranslationPath(ctx context.Context, dir string, site string, relative bool) (string, error)
	GetUpdateHooks(ctx context.Context, dir string, site string) (map[string]UpdateHook, error)
}

// CLI is the default implementation of CommandExecutor
type CLI struct {
	logger *zap.Logger
	cache  otter.Cache[string, string]
	fs     afero.Fs
}

func NewCLI(logger *zap.Logger, cache otter.Cache[string, string]) Runner {
	return CLI{
		logger: logger,
		cache:  cache,
		fs:     afero.NewOsFs(),
	}
}

func (e CLI) ExecDrush(ctx context.Context, dir string, site string, args ...string) (string, error) {
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

func (e CLI) InstallSite(ctx context.Context, dir string, site string) error {
	e.logger.Debug("installing site")
	_, err := e.ExecDrush(ctx, dir, site, "--existing-config", "--yes", "site:install", "--sites-subdir="+site)

	return err
}

func (e CLI) GetConfigSyncDir(ctx context.Context, dir string, site string, relative bool) (string, error) {
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

func (e CLI) ExportConfiguration(ctx context.Context, dir string, site string) error {
	e.logger.Debug("exporting configuration")
	_, err := e.ExecDrush(ctx, dir, site, "config:export", "--yes")
	return err
}

func (e CLI) UpdateSite(ctx context.Context, dir string, site string) error {
	e.logger.Debug("updating site")
	_, err := e.ExecDrush(ctx, dir, site, "updatedb", "--yes", "-vvv")
	return err
}

func (e CLI) ConfigResave(ctx context.Context, dir string, site string) error {
	e.logger.Debug("config resave")
	_, err := e.ExecDrush(ctx, dir, site, "php:script", "/opt/drupdater/config-resave.php")
	return err
}

func (e CLI) IsModuleEnabled(ctx context.Context, dir string, site string, module string) (bool, error) {
	e.logger.Debug("checking if module is enabled")
	out, err := e.ExecDrush(ctx, dir, site, "pm:list", "--status=enabled", "--field=name", "--filter="+module)
	return out == module, err
}

func (e CLI) LocalizeTranslations(ctx context.Context, dir string, site string) error {
	e.logger.Debug("localizing translations")
	_, err := e.ExecDrush(ctx, dir, site, "locale-deploy:localize-translations")
	return err
}

func (e CLI) GetTranslationPath(ctx context.Context, dir string, site string, relative bool) (string, error) {
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

type UpdateHook struct {
	Module      string      `json:"module"`
	UpdateID    interface{} `json:"update_id"`
	Description string      `json:"description"`
	Type        string      `json:"type"`
}

func (e CLI) GetUpdateHooks(ctx context.Context, dir string, site string) (map[string]UpdateHook, error) {
	e.logger.Debug("getting update hooks")
	data, err := e.ExecDrush(ctx, dir, site, "updatedb-status", "--format=json")
	if err != nil {
		return nil, err
	}

	if strings.Contains(data, "No database updates required") {
		return nil, nil
	}

	var updates map[string]UpdateHook
	if err := json.Unmarshal([]byte(data), &updates); err != nil {
		e.logger.Error("failed to unmarshal update hooks", zap.Error(err))
		return nil, err
	}

	return updates, nil
}
