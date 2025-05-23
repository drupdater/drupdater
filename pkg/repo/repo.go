package repo

import (
	"crypto/md5"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/afero"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"go.uber.org/zap"
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

type GitRepositoryService struct {
	logger *zap.Logger
	fs     afero.Fs
}

func NewGitRepositoryService(logger *zap.Logger) *GitRepositoryService {
	return &GitRepositoryService{
		logger: logger,
		fs:     afero.NewOsFs(),
	}
}

func (rs *GitRepositoryService) CloneRepository(repository string, branch string, token string, username string, email string) (Repository, Worktree, string, error) {

	hash := fmt.Sprintf("%x", md5.Sum([]byte(repository)))
	projectDir := filepath.Join(os.TempDir(), hash)
	if err := rs.fs.MkdirAll(projectDir, os.ModePerm); err != nil {
		return nil, nil, "", fmt.Errorf("failed to create project directory: %w", err)
	}
	tmpDirName, err := afero.TempDir(rs.fs, projectDir, "repo")
	if err != nil {
		return nil, nil, "", fmt.Errorf("failed to create temporary directory: %w", err)
	}

	checkout, err := git.PlainClone(tmpDirName, false, &git.CloneOptions{
		URL:           repository,
		Depth:         1,
		ReferenceName: plumbing.NewBranchReferenceName(branch),
		Auth: &http.BasicAuth{
			Username: "du", // yes, this can be anything except an empty string
			Password: token,
		},
		Tags: git.NoTags,
	})

	if err != nil {
		return nil, nil, "", fmt.Errorf("git clone: %w", err)
	}

	// Set the user name and email for the commit
	config, _ := checkout.Config()
	config.User.Name = username
	config.User.Email = email
	err = checkout.SetConfig(config)
	if err != nil {
		return checkout, nil, "", err
	}

	w, err := checkout.Worktree()
	if err != nil {
		return checkout, nil, "", err
	}

	// @TODO: Verify if this is necessary
	// Remove prepare-commit-msg hook because it does not work with the --no-verify option.
	if _, err := w.Filesystem.Stat(".git/hooks/prepare-commit-msg"); err == nil {
		err = w.Filesystem.Remove(".git/hooks/prepare-commit-msg")
		if err != nil {
			return checkout, w, "", fmt.Errorf("failed to remove prepare-commit-msg hook: %w", err)
		}
	}

	return checkout, w, w.Filesystem.Root(), nil
}

func (rs *GitRepositoryService) BranchExists(repository Repository, branch string) (bool, error) {
	// Get list of remote branches
	remoteRefs, err := repository.References()
	if err != nil {
		return false, err
	}

	// Iterate through the references and check if branch exists
	remoteBranchRef := fmt.Sprintf("refs/remotes/origin/%s", branch)

	for {
		ref, err := remoteRefs.Next()
		if err != nil {
			break // End of refs
		}

		if ref.Name().String() == remoteBranchRef {
			return true, nil
		}
	}
	return false, nil
}

func (rs *GitRepositoryService) IsSomethingStagedInPath(worktree Worktree, dir string) bool {
	status, err := worktree.Status()
	if err != nil {
		rs.logger.Error("failed to get worktree status", zap.Error(err))
		return false
	}

	for filePath, s := range status {
		if s.Staging != git.Unmodified && strings.Contains(filePath, dir) {
			return true
		}
	}

	return false
}
