package drush

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/maypok86/otter"
	"go.uber.org/zap"
)

var execCommand = exec.CommandContext

// CLI is the default implementation of CommandExecutor
type CLI struct {
	logger *zap.Logger
	cache  otter.Cache[string, string]
}

func NewCLI(logger *zap.Logger, cache otter.Cache[string, string]) *CLI {
	return &CLI{
		logger: logger,
		cache:  cache,
	}
}

func (e *CLI) execDrush(ctx context.Context, dir string, site string, args ...string) (string, error) {
	command := execCommand(ctx, "composer", append([]string{"exec", "--", "drush"}, args...)...)
	command.Dir = dir
	// System env first, then our override so SITE_NAME always wins even if
	// the parent process has SITE_NAME set in its environment.
	command.Env = append(os.Environ(), "SITE_NAME="+site)

	out, err := command.CombinedOutput()
	output := strings.TrimSuffix(string(out), "\n")

	e.logger.Debug(command.String() + "\n" + output)

	return output, err
}

// execDrushStreams runs drush and returns stdout and stderr separately. Commands whose stdout
// is parsed as JSON must use this: drush writes notices to stderr, and folding them into stdout
// (as CombinedOutput does) would corrupt the JSON.
func (e *CLI) execDrushStreams(ctx context.Context, dir string, site string, args ...string) (stdout string, stderr string, err error) {
	command := execCommand(ctx, "composer", append([]string{"exec", "--", "drush"}, args...)...)
	command.Dir = dir
	command.Env = append(os.Environ(), "SITE_NAME="+site)

	var so, se bytes.Buffer
	command.Stdout = &so
	command.Stderr = &se
	err = command.Run()

	stdout = strings.TrimSuffix(so.String(), "\n")
	stderr = strings.TrimSuffix(se.String(), "\n")
	e.logger.Debug(command.String() + "\nstdout: " + stdout + "\nstderr: " + stderr)

	return stdout, stderr, err
}

func (e *CLI) InstallSite(ctx context.Context, dir string, site string) error {
	out, err := e.execDrush(ctx, dir, site, "--existing-config", "--yes", "site:install", "--sites-subdir="+site, "--verbose")
	if err != nil {
		return fmt.Errorf("failed to install %s: %w, output: %s", site, err, out)
	}
	return err
}

func (e *CLI) GetConfigSyncDir(ctx context.Context, dir string, site string, relative bool) (string, error) {
	cacheKey := fmt.Sprintf("config-sync-dir_%s_%s_%t", dir, site, relative)
	value, ok := e.cache.Get(cacheKey)
	if ok {
		return value, nil
	}
	configSyncDir, err := e.execDrush(ctx, dir, site, "ev", "print realpath(\\Drupal\\Core\\Site\\Settings::get('config_sync_directory'))")
	if err != nil {
		return "", err
	}
	if relative {
		configSyncDir = strings.TrimLeft(strings.TrimPrefix(configSyncDir, dir), "/")
	}
	e.cache.Set(cacheKey, configSyncDir)
	return configSyncDir, nil
}

func (e *CLI) ExportConfiguration(ctx context.Context, dir string, site string) error {
	_, err := e.execDrush(ctx, dir, site, "config:export", "--yes", "--commit", "--message=Update configuration "+site)
	return err
}

func (e *CLI) UpdateSite(ctx context.Context, dir string, site string) error {
	_, err := e.execDrush(ctx, dir, site, "updatedb", "--yes")
	return err
}

func (e *CLI) ConfigResave(ctx context.Context, dir string, site string) error {
	_, err := e.execDrush(ctx, dir, site, "php:script", "/opt/drupdater/config-resave.php")
	return err
}

func (e *CLI) IsModuleEnabled(ctx context.Context, dir string, site string, module string) (bool, error) {
	out, err := e.execDrush(ctx, dir, site, "pm:list", "--status=enabled", "--field=name", "--filter="+module)
	return out == module, err
}

func (e *CLI) LocalizeTranslations(ctx context.Context, dir string, site string) error {
	_, err := e.execDrush(ctx, dir, site, "locale-deploy:localize-translations")
	return err
}

func (e *CLI) GetTranslationPath(ctx context.Context, dir string, site string, relative bool) (string, error) {
	cacheKey := fmt.Sprintf("translation-path_%s_%s_%t", dir, site, relative)
	value, ok := e.cache.Get(cacheKey)
	if ok {
		return value, nil
	}
	translationPath, err := e.execDrush(ctx, dir, site, "ev", "print realpath(\\Drupal::config('locale.settings')->get('translation.path'))")
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
	Module      string `json:"module"`
	UpdateID    any    `json:"update_id"`
	Description string `json:"description"`
	Type        string `json:"type"`
}

func (e *CLI) GetUpdateHooks(ctx context.Context, dir string, site string) (map[string]UpdateHook, error) {
	stdout, stderr, err := e.execDrushStreams(ctx, dir, site, "updatedb-status", "--format=json")
	if err != nil {
		return nil, err
	}

	// "No database updates required" is a drush notice; depending on the version it lands on
	// stdout or stderr. An empty stdout means the same thing (nothing to parse).
	if strings.Contains(stdout, "No database updates required") ||
		strings.Contains(stderr, "No database updates required") ||
		strings.TrimSpace(stdout) == "" {
		return nil, nil
	}

	var updates map[string]UpdateHook
	if err := json.Unmarshal([]byte(stdout), &updates); err != nil {
		return nil, fmt.Errorf("failed to unmarshal update hooks: %w", err)
	}

	return updates, nil
}
