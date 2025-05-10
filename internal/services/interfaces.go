package services

import (
	"context"

	"github.com/drupdater/drupdater/internal/codehosting"
	"github.com/drupdater/drupdater/pkg/composer"
	"github.com/drupdater/drupdater/pkg/repo"
	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/storer"
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

type GitRepository interface {
	Push(o *git.PushOptions) error
	Head() (*plumbing.Reference, error)
	CommitObject(h plumbing.Hash) (*object.Commit, error)
	References() (storer.ReferenceIter, error)
}

type Worktree interface {
	Add(path string) (plumbing.Hash, error)
	AddGlob(pattern string) error
	Remove(path string) (plumbing.Hash, error)
	Commit(msg string, opts *git.CommitOptions) (plumbing.Hash, error)
	Status() (git.Status, error)
	Checkout(opts *git.CheckoutOptions) error
}

type Installer interface {
	Install(ctx context.Context, dir string, site string) error
	ConfigureDatabase(ctx context.Context, dir string, site string) error
}

type Platform interface {
	CreateMergeRequest(title string, description string, sourceBranch string, targetBranch string) (codehosting.MergeRequest, error)
	DownloadComposerFiles(branch string) string
	GetUser() (name string, email string)
}
