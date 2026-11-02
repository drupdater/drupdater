package workflow

import (
	"context"
	"encoding/json"

	"github.com/drupdater/drupdater/internal/codehosting"
	"github.com/drupdater/drupdater/internal/composer"
	"github.com/drupdater/drupdater/internal/repo"
)

// Composer is what the phases call, plus what they hand to the preflight checks and the report.
type Composer interface {
	Install(ctx context.Context, dir string) error
	Update(ctx context.Context, dir string, packagesToUpdate []string, packagesToKeep []string, minimalChanges bool) ([]composer.PackageChange, error)
	AddonAgent
	PlatformReqsChecker
	AgentChecker
	ProjectReader
	ComposerVersionReporter
}

// Sites is what the phases do to one site, through the agent. Facts are as the agent's
// drupdater:facts printed them, handed to the addons unread.
type Sites interface {
	Facts(ctx context.Context, dir string, site string) (json.RawMessage, error)
	// Update runs the update hooks and returns the facts as they stand afterwards.
	Update(ctx context.Context, dir string, site string) (json.RawMessage, error)
	// ExportConfiguration returns the directory it exported into, relative to dir.
	ExportConfiguration(ctx context.Context, dir string, site string) (string, error)
	Install(ctx context.Context, dir string, site string) error
	ConfigureDatabase(ctx context.Context, dir string, site string) error
	RestoreSettings(ctx context.Context, dir string, site string) error
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

type Platform interface {
	CreateMergeRequest(ctx context.Context, title string, description string, sourceBranch string, targetBranch string) (codehosting.MergeRequest, error)
	DeleteBranch(ctx context.Context, branch string) error
	GetUser(ctx context.Context) (name string, email string)
	EnableAutoMerge(ctx context.Context, mr codehosting.MergeRequest) error
}
