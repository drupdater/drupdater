package addon

import (
	"context"

	"github.com/drupdater/drupdater/pkg/composer"
	"github.com/drupdater/drupdater/pkg/drupalorg"
	"github.com/drupdater/drupdater/pkg/drush"
	"github.com/drupdater/drupdater/pkg/phpcs"
	"github.com/drupdater/drupdater/pkg/rector"
	"github.com/drupdater/drupdater/pkg/repo"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

type Composer interface {
	Update(ctx context.Context, dir string, packagesToUpdate []string, packagesToKeep []string, minimalChanges bool, dryRun bool) ([]composer.PackageChange, error)
	Require(ctx context.Context, dir string, args ...string) (string, error)
	Remove(ctx context.Context, dir string, packages ...string) (string, error)
	Audit(ctx context.Context, dir string) (composer.Audit, error)
	Normalize(ctx context.Context, dir string) (string, error)
	Diff(ctx context.Context, path string, withLinks bool) (string, error)

	GetInstalledPackageVersion(ctx context.Context, dir string, packageName string) (string, error)
	GetAllowPlugins(ctx context.Context, dir string) (map[string]bool, error)
	SetAllowPlugins(ctx context.Context, dir string, plugins map[string]bool) error
	GetConfig(ctx context.Context, dir string, key string) (string, error)
	SetConfig(ctx context.Context, dir string, key string, value string) error

	CheckIfPatchApplies(ctx context.Context, packageName string, packageVersion string, patchPath string) (bool, error)
	GetInstalledPlugins(ctx context.Context, dir string) (map[string]interface{}, error)
	IsPackageInstalled(ctx context.Context, dir string, packageToCheck string) (bool, error)
	UpdateLockHash(ctx context.Context, dir string) error
	GetCustomCodeDirectories(ctx context.Context, dir string) ([]string, error)
}

type Drush interface {
	IsModuleEnabled(ctx context.Context, dir string, site string, module string) (bool, error)
	LocalizeTranslations(ctx context.Context, dir string, site string) error
	GetTranslationPath(ctx context.Context, dir string, site string, relative bool) (string, error)
	GetUpdateHooks(ctx context.Context, dir string, site string) (map[string]drush.UpdateHook, error)
}

type PHPCS interface {
	Run(ctx context.Context, path string) (phpcs.ReturnOutput, error)
	RunCBF(ctx context.Context, path string) error
}

type Repository interface {
	IsSomethingStagedInPath(worktree repo.Worktree, dir string) bool
}

type DrupalOrg interface {
	GetIssue(issueID string) (*drupalorg.Issue, error)
	FindIssueNumber(text string) (string, bool)
}

type Rector interface {
	Run(ctx context.Context, dir string, customCodeDirectories []string) (rector.ReturnOutput, error)
}

type Worktree interface {
	Add(path string) (plumbing.Hash, error)
	AddGlob(pattern string) error
	Remove(path string) (plumbing.Hash, error)
	Commit(msg string, opts *git.CommitOptions) (plumbing.Hash, error)
	Status() (git.Status, error)
	Checkout(opts *git.CheckoutOptions) error
}
