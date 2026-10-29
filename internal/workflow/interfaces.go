package workflow

import (
	"context"

	"github.com/drupdater/drupdater/internal/codehosting"
	"github.com/drupdater/drupdater/pkg/composer"
	"github.com/drupdater/drupdater/pkg/repo"
)

// Addon hooks into workflow events. What it found reaches the merge request only through its
// report section: the description is rendered from the report.
type Addon interface {
	// Hooks is read once per run, before the first event fires.
	Hooks() Hooks
}

type Composer interface {
	Install(ctx context.Context, dir string) error
	Update(ctx context.Context, dir string, packagesToUpdate []string, packagesToKeep []string, minimalChanges bool, dryRun bool) ([]composer.PackageChange, error)
	GetLockHash(dir string) (string, error)
	CheckPlatformReqs(ctx context.Context, dir string) (string, error)
	GetConfig(ctx context.Context, dir string, key string) (string, error)
	Version(ctx context.Context) (composer.Versions, error)
}

type Drush interface {
	UpdateSite(ctx context.Context, dir string, site string) error
	ExportConfiguration(ctx context.Context, dir string, site string) error
	GetConfigSyncDir(ctx context.Context, dir string, site string, relative bool) (string, error)
	ConfigResave(ctx context.Context, dir string, site string) error
}

type Repository interface {
	BranchExists(repository repo.Repository, branch string, token string) (bool, error)
	CloneRepository(repository string, branch string, token string, username string, email string) (repo.Checkout, error)
	AddWorktree(ctx context.Context, source string, username string, email string) (repo.Checkout, error)
	RemoveWorktree(ctx context.Context, source string, path string) error
	GetRemoteURL(path string) (string, error)
	GetCurrentBranch(path string) (string, error)
	IsShallowClone(path string) (bool, error)
}

// GitRepository is an alias for repo.Repository to avoid duplication.
type GitRepository = repo.Repository

// Worktree is an alias for repo.Worktree to avoid duplication.
type Worktree = repo.Worktree

type Installer interface {
	Install(ctx context.Context, dir string, site string) error
	ConfigureDatabase(ctx context.Context, dir string, site string) error
	RestoreSettings(ctx context.Context, dir string, site string) error
}

type Platform interface {
	CreateMergeRequest(ctx context.Context, title string, description string, sourceBranch string, targetBranch string) (codehosting.MergeRequest, error)
	DeleteBranch(ctx context.Context, branch string) error
	GetUser(ctx context.Context) (name string, email string)
	EnableAutoMerge(ctx context.Context, mr codehosting.MergeRequest) error
}
