package addon

import (
	"context"
	"net/http"

	"github.com/drupdater/drupdater/pkg/composer"
	"github.com/drupdater/drupdater/pkg/drupalorg"
	"github.com/drupdater/drupdater/pkg/drush"
	"github.com/drupdater/drupdater/pkg/phpcs"
	"github.com/drupdater/drupdater/pkg/rector"
)

type Composer interface {
	Update(ctx context.Context, dir string, packagesToUpdate []string, packagesToKeep []string, minimalChanges bool, dryRun bool) ([]composer.PackageChange, error)
	Audit(ctx context.Context, dir string) (composer.Audit, error)
	Normalize(ctx context.Context, dir string) (string, error)
	Diff(ctx context.Context, path string) ([]composer.DiffEntry, error)

	GetInstalledPackageVersion(ctx context.Context, dir string, packageName string) (string, error)
	GetAllowPlugins(ctx context.Context, dir string) (map[string]bool, error)
	SetAllowPlugins(ctx context.Context, dir string, plugins map[string]bool) error
	GetConfig(ctx context.Context, dir string, key string) (string, error)
	SetConfig(ctx context.Context, dir string, key string, value string) error
	GetDependencyPatches(ctx context.Context, dir string) (map[string]map[string]bool, error)

	CheckIfPatchApplies(ctx context.Context, dir string, packageName string, packageVersion string, patchPath string) (bool, error)
	CheckIfPatchesApply(ctx context.Context, dir string, packageName string, packageVersion string, patchPaths []string) (bool, error)
	GetInstalledPlugins(ctx context.Context, dir string) (map[string]any, error)
	IsPackageInstalled(ctx context.Context, dir string, packageToCheck string) (bool, error)
	UpdateLockHash(ctx context.Context, dir string) error
	GetCustomCodeDirectories(ctx context.Context, dir string) ([]string, error)
}

type Drush interface {
	IsModuleEnabled(ctx context.Context, dir string, site string, module string) (bool, error)
	LocalizeTranslations(ctx context.Context, dir string, site string) error
	GetTranslationPath(ctx context.Context, dir string, site string, relative bool) (string, error)
	GetUpdateHooks(ctx context.Context, dir string, site string) (map[string]drush.UpdateHook, error)
	GetUnsupportedModules(ctx context.Context, dir string, site string) ([]drush.UnsupportedModule, error)
}

type PHPCS interface {
	Run(ctx context.Context, path string) (phpcs.ReturnOutput, error)
	RunCBF(ctx context.Context, path string) error
}

type DrupalOrg interface {
	GetIssue(ctx context.Context, issueID string) (*drupalorg.Issue, error)
	FindIssueNumber(text string) (string, bool)
}

type Rector interface {
	Run(ctx context.Context, dir string, customCodeDirectories []string) (rector.ReturnOutput, error)
}

type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}
