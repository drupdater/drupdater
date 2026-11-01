package workflow

import (
	"context"

	"github.com/drupdater/drupdater/internal/codehosting"
	"github.com/drupdater/drupdater/internal/composer"
	"github.com/drupdater/drupdater/internal/drush"
	"github.com/drupdater/drupdater/internal/repo"
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
	AgentChecker
	ComposerConfigGetter
	ComposerVersionReporter
}

type Drush interface {
	Facts(ctx context.Context, dir string, site string) (drush.Facts, error)
	LocalFacts(ctx context.Context, dir string, site string) (drush.Facts, error)
	UpdateSite(ctx context.Context, dir string, site string) error
	ExportConfiguration(ctx context.Context, dir string, site string) error
	ConfigResave(ctx context.Context, dir string, site string) error
}

// GitService clones and inspects repositories. A run's own repository is a GitRepository.
type GitService interface {
	CloneRepository(ctx context.Context, repository string, branch string, token string, requested repo.Identity) (repo.Checkout, error)
	AddWorktree(ctx context.Context, source string, requested repo.Identity) (repo.Checkout, error)
	RemoveWorktree(ctx context.Context, source string, path string) error
	ShallowCloneChecker
}

// GitRepository and Worktree alias repo's types so mockery generates their mocks in this package.
type (
	GitRepository = repo.Repository
	Worktree      = repo.Worktree
)

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
