package repo

import (
	"crypto/md5"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/afero"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"go.uber.org/zap"
)

type Repository interface {
	Push(o *git.PushOptions) error
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

	return prepareCheckout(checkout, username, email)
}

// OpenRepository opens an existing checkout (e.g. the one CI already provides) instead of
// cloning. It applies the same git-user and hook setup as CloneRepository so commits and
// pushes behave identically.
func (rs *GitRepositoryService) OpenRepository(path string, username string, email string) (Repository, Worktree, string, error) {
	checkout, err := git.PlainOpen(path)
	if err != nil {
		return nil, nil, "", fmt.Errorf("git open %q: %w", path, err)
	}
	return prepareCheckout(checkout, username, email)
}

// GetRemoteURL returns the "origin" remote URL of the checkout at path. It is how checkout
// mode learns the repository URL (for GitHub/GitLab detection) without requiring it as an
// argument. Any embedded credentials (e.g. GitLab CI's token in the URL) are stripped.
func (rs *GitRepositoryService) GetRemoteURL(path string) (string, error) {
	checkout, err := git.PlainOpen(path)
	if err != nil {
		return "", fmt.Errorf("git open %q: %w", path, err)
	}
	remote, err := checkout.Remote("origin")
	if err != nil {
		return "", fmt.Errorf("failed to get origin remote: %w", err)
	}
	urls := remote.Config().URLs
	if len(urls) == 0 {
		return "", fmt.Errorf("origin remote has no URL")
	}
	if u, err := url.Parse(urls[0]); err == nil {
		u.User = nil
		return u.String(), nil
	}
	return urls[0], nil
}

// GetCurrentBranch returns the short name of the branch HEAD points to in the checkout at
// path, or "" if HEAD is detached (the usual state of a CI checkout). Callers fall back to
// CI environment variables in that case.
func (rs *GitRepositoryService) GetCurrentBranch(path string) (string, error) {
	checkout, err := git.PlainOpen(path)
	if err != nil {
		return "", fmt.Errorf("git open %q: %w", path, err)
	}
	head, err := checkout.Head()
	if err != nil {
		return "", fmt.Errorf("failed to get HEAD: %w", err)
	}
	if head.Name().IsBranch() {
		return head.Name().Short(), nil
	}
	return "", nil
}

// prepareCheckout sets the commit identity and removes the prepare-commit-msg hook, then
// returns the repository, worktree and working-tree root. Shared by clone and open.
func prepareCheckout(checkout *git.Repository, username string, email string) (Repository, Worktree, string, error) {
	config, _ := checkout.Config()
	config.User.Name = username
	config.User.Email = email
	if err := checkout.SetConfig(config); err != nil {
		return checkout, nil, "", err
	}

	w, err := checkout.Worktree()
	if err != nil {
		return checkout, nil, "", err
	}

	// @TODO: Verify if this is necessary
	// Remove prepare-commit-msg hook because it does not work with the --no-verify option.
	if _, err := w.Filesystem.Stat(".git/hooks/prepare-commit-msg"); err == nil {
		if err := w.Filesystem.Remove(".git/hooks/prepare-commit-msg"); err != nil {
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
		if err == io.EOF {
			break
		}
		if err != nil {
			return false, fmt.Errorf("iterating remote refs: %w", err)
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
