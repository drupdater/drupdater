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

// Composer is what the phases call, plus what they hand to the preflight checks and the report.
type Composer interface {
	Install(ctx context.Context, dir string) error
	Update(ctx context.Context, dir string, packagesToUpdate []string, packagesToKeep []string, minimalChanges bool, dryRun bool) ([]composer.PackageChange, error)
	GetLockHash(dir string) (string, error)
	PlatformReqsChecker
	ComposerConfigGetter
	ComposerVersionReporter
}

type Drush interface {
	UpdateSite(ctx context.Context, dir string, site string) error
	ExportConfiguration(ctx context.Context, dir string, site string) error
	GetConfigSyncDir(ctx context.Context, dir string, site string, relative bool) (string, error)
	ConfigResave(ctx context.Context, dir string, site string) error
}

// GitService clones and inspects repositories. A run's own repository is a GitRepository.
type GitService interface {
	BranchExists(repository repo.Repository, branch string, token string) (bool, error)
	CloneRepository(repository string, branch string, token string, username string, email string) (repo.Checkout, error)
	AddWorktree(ctx context.Context, source string, username string, email string) (repo.Checkout, error)
	RemoveWorktree(ctx context.Context, source string, path string) error
	ShallowCloneChecker
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
