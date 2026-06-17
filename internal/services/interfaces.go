package services

import (
	"context"

	"github.com/drupdater/drupdater/internal/codehosting"
	"github.com/drupdater/drupdater/pkg/composer"
	"github.com/drupdater/drupdater/pkg/repo"
	"github.com/gookit/event"
)

type Composer interface {
	Install(ctx context.Context, dir string) error
	Update(ctx context.Context, dir string, packagesToUpdate []string, packagesToKeep []string, minimalChanges bool, dryRun bool) ([]composer.PackageChange, error)
	GetLockHash(dir string) (string, error)
}

type Drush interface {
	UpdateSite(ctx context.Context, dir string, site string) error
	ExportConfiguration(ctx context.Context, dir string, site string) error
	ConfigResave(ctx context.Context, dir string, site string) error
}

type Repository interface {
	BranchExists(repository repo.Repository, branch string) (bool, error)
	CloneRepository(repository string, branch string, token string, username string, email string) (repo.Repository, repo.Worktree, string, error)
}

// GitRepository is an alias for repo.Repository to avoid duplication.
type GitRepository = repo.Repository

// Worktree is an alias for repo.Worktree to avoid duplication.
type Worktree = repo.Worktree

type Installer interface {
	Install(ctx context.Context, dir string, site string) error
	ConfigureDatabase(ctx context.Context, dir string, site string) error
}

type Platform interface {
	CreateMergeRequest(title string, description string, sourceBranch string, targetBranch string) (codehosting.MergeRequest, error)
	DownloadComposerFiles(branch string) string
	GetUser() (name string, email string)
}

// EventDispatcher abstracts the event bus so it can be injected and tested independently.
type EventDispatcher interface {
	FireEvent(e event.Event) error
	AddSubscriber(subscriber event.Subscriber)
}
