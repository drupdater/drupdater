package internal

import (
	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/storer"
)

type Repository interface {
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
