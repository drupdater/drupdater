package drush

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/maypok86/otter"
	"go.uber.org/zap"

	"github.com/drupdater/drupdater/pkg/composer"
)

var execCommand = exec.CommandContext

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

// command runs drush through `composer exec`, inheriting its process timeout. SITE_NAME comes
// after that environment so it wins over any value the parent process set.
func (e *CLI) command(dir string, site string) composer.Command {
	return composer.Command{
		New:      execCommand,
		Logger:   e.logger,
		Dir:      dir,
		ExtraEnv: []string{"SITE_NAME=" + site},
	}
}

func drushArgs(args []string) []string {
	return append([]string{"exec", "--", "drush"}, args...)
}

func (e *CLI) execDrush(ctx context.Context, dir string, site string, args ...string) (string, error) {
	return e.command(dir, site).Combined(ctx, drushArgs(args)...)
}

// execDrushStreams keeps the streams apart, so a stderr notice cannot corrupt a JSON payload.
func (e *CLI) execDrushStreams(ctx context.Context, dir string, site string, args ...string) (stdout string, stderr string, err error) {
	return e.command(dir, site).Split(ctx, drushArgs(args)...)
}

func (e *CLI) InstallSite(ctx context.Context, dir string, site string) error {
	out, err := e.execDrush(ctx, dir, site, "--existing-config", "--yes", "site:install", "--sites-subdir="+site, "--verbose")
	if err != nil {
		return fmt.Errorf("failed to install %s: %w, output: %s", site, err, out)
	}
	return nil
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

// IsModuleEnabled splits the streams: a merged notice would report an enabled module as disabled.
func (e *CLI) IsModuleEnabled(ctx context.Context, dir string, site string, module string) (bool, error) {
	stdout, stderr, err := e.execDrushStreams(ctx, dir, site, "pm:list", "--status=enabled", "--field=name", "--filter="+module)
	if err != nil {
		return false, fmt.Errorf("failed to check if module %s is enabled: %w, output: %s", module, err, stderr)
	}
	return strings.TrimSpace(stdout) == module, nil
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
	// An empty path must never reach git: Worktree.Add would stage the entire working tree.
	// realpath() prints nothing when the directory was never created.
	if strings.TrimSpace(translationPath) == "" {
		return "", fmt.Errorf("translation path for site %s does not resolve to an existing directory", site)
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

// UnsupportedModule is an installed module with no supported release: no further fixes coming.
type UnsupportedModule struct {
	Name               string `json:"name"`
	InstalledVersion   string `json:"installed_version"`
	RecommendedVersion string `json:"recommended_version"`
}

// GetUnsupportedModules runs the bundled unsupported-modules.php, which yields nothing when
// Drupal's update module is off.
func (e *CLI) GetUnsupportedModules(ctx context.Context, dir string, site string) ([]UnsupportedModule, error) {
	stdout, stderr, err := e.execDrushStreams(ctx, dir, site, "php:script", "/opt/drupdater/unsupported-modules.php")
	if err != nil {
		return nil, fmt.Errorf("failed to check for unsupported modules: %w, output: %s", err, stderr)
	}

	if strings.TrimSpace(stdout) == "" {
		return nil, nil
	}

	var modules []UnsupportedModule
	if err := json.Unmarshal([]byte(stdout), &modules); err != nil {
		return nil, fmt.Errorf("failed to unmarshal unsupported modules: %w", err)
	}

	return modules, nil
}

func (e *CLI) GetUpdateHooks(ctx context.Context, dir string, site string) (map[string]UpdateHook, error) {
	stdout, stderr, err := e.execDrushStreams(ctx, dir, site, "updatedb-status", "--format=json")
	if err != nil {
		return nil, err
	}

	// "No database updates required" lands on either stream, depending on the drush version.
	if strings.Contains(stdout, "No database updates required") ||
		strings.Contains(stderr, "No database updates required") ||
		strings.TrimSpace(stdout) == "" {
		return map[string]UpdateHook{}, nil
	}

	var updates map[string]UpdateHook
	if err := json.Unmarshal([]byte(stdout), &updates); err != nil {
		return nil, fmt.Errorf("failed to unmarshal update hooks: %w", err)
	}

	return updates, nil
}
