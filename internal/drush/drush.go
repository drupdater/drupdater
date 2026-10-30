package drush

import (
	"context"
	"fmt"
	"maps"
	"os/exec"
	"path/filepath"
	"slices"

	"github.com/drupdater/drupdater/internal/php"

	"go.uber.org/zap"
)

type CLI struct {
	logger     *zap.Logger
	newCommand php.CommandFactory
	// include holds drupdater's own drush commands, passed to drush --include.
	include string
}

func NewCLI(logger *zap.Logger, include string) *CLI {
	return &CLI{
		logger:     logger,
		newCommand: exec.CommandContext,
		include:    include,
	}
}

// command runs drush through `composer exec`, inheriting its process timeout. SITE_NAME comes
// after that environment so it wins over any value the parent process set.
func (e *CLI) command(dir string, site string) php.Command {
	return php.Command{
		New:      e.newCommand,
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

func (e *CLI) InstallSite(ctx context.Context, dir string, site string) error {
	out, err := e.execDrush(ctx, dir, site, "--existing-config", "--yes", "site:install", "--sites-subdir="+site, "--verbose")
	if err != nil {
		return fmt.Errorf("failed to install %s: %w, output: %s", site, err, out)
	}
	return nil
}

// GetConfigSyncDir returns the site's absolute config sync directory. Unlike Facts it needs no
// database, so it works before the site is installed.
func (e *CLI) GetConfigSyncDir(ctx context.Context, dir string, site string) (string, error) {
	status, err := php.JSON[struct {
		ConfigSync string `json:"config-sync"`
		Root       string `json:"root"`
	}](ctx, e.command(dir, site), drushArgs([]string{"status", "--fields=config-sync,root", "--format=json"})...)
	if err != nil {
		return "", fmt.Errorf("failed to read the config sync directory of site %s: %w", site, err)
	}
	if status.ConfigSync == "" {
		return "", fmt.Errorf("site %s has no config sync directory", site)
	}
	if filepath.IsAbs(status.ConfigSync) {
		return status.ConfigSync, nil
	}
	return filepath.Join(status.Root, status.ConfigSync), nil
}

func (e *CLI) ExportConfiguration(ctx context.Context, dir string, site string) error {
	_, err := e.execDrush(ctx, dir, site, "config:export", "--yes")
	return err
}

func (e *CLI) UpdateSite(ctx context.Context, dir string, site string) error {
	_, err := e.execDrush(ctx, dir, site, "updatedb", "--yes")
	return err
}

func (e *CLI) ConfigResave(ctx context.Context, dir string, site string) error {
	_, err := e.execDrush(ctx, dir, site, "--include="+e.include, "drupdater:config-resave")
	return err
}

func (e *CLI) LocalizeTranslations(ctx context.Context, dir string, site string) error {
	_, err := e.execDrush(ctx, dir, site, "locale-deploy:localize-translations")
	return err
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

// Facts is what drupdater reads from a site, in one bootstrap. Paths are absolute and may not
// exist yet.
type Facts struct {
	ConfigSyncDirectory string                `json:"config_sync_directory"`
	TranslationPath     string                `json:"translation_path"`
	Modules             []string              `json:"modules"`
	UpdateHooks         map[string]UpdateHook `json:"update_hooks"`
	UnsupportedModules  []UnsupportedModule   `json:"unsupported_modules"`
	// UnsupportedModulesError is why the update status service could not be asked, if it could not.
	UnsupportedModulesError string `json:"unsupported_modules_error"`
}

func (f Facts) ModuleEnabled(module string) bool {
	return slices.Contains(f.Modules, module)
}

// Facts runs drupdater:facts, which reads the site under drush's update kernel: the code is
// already updated, the schema is not.
func (e *CLI) Facts(ctx context.Context, dir string, site string) (Facts, error) {
	return e.facts(ctx, dir, site)
}

// LocalFacts is Facts without the drupal.org request, so UnsupportedModules stays empty.
func (e *CLI) LocalFacts(ctx context.Context, dir string, site string) (Facts, error) {
	return e.facts(ctx, dir, site, "--skip-update-status")
}

func (e *CLI) facts(ctx context.Context, dir string, site string, options ...string) (Facts, error) {
	args := append([]string{"--include=" + e.include, "drupdater:facts"}, options...)
	facts, err := php.JSON[Facts](ctx, e.command(dir, site), drushArgs(args)...)
	if err != nil {
		return Facts{}, fmt.Errorf("failed to read the facts of site %s: %w", site, err)
	}
	return facts, nil
}

// Clone copies the slices and the map, so no holder can change another's facts.
func (f Facts) Clone() Facts {
	f.Modules = slices.Clone(f.Modules)
	f.UpdateHooks = maps.Clone(f.UpdateHooks)
	f.UnsupportedModules = slices.Clone(f.UnsupportedModules)
	return f
}
