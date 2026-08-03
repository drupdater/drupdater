package repo

import (
	"fmt"
	"hash/fnv"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/afero"

	git "github.com/go-git/go-git/v5"
	gitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"go.uber.org/zap"
)

// Last-resort commit identity, when neither the platform nor the checkout supplies one.
const (
	defaultCommitName  = "drupdater"
	defaultCommitEmail = "drupdater@localhost"
)

type Repository interface {
	Push(o *git.PushOptions) error
	Remote(name string) (*git.Remote, error)
	// Head resolves to a branch, or to a commit hash when detached — the usual CI state.
	Head() (*plumbing.Reference, error)
	// Reference looks up a local reference, erroring with plumbing.ErrReferenceNotFound.
	Reference(name plumbing.ReferenceName, resolved bool) (*plumbing.Reference, error)
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

// BasicAuth is the credential go-git needs for an authenticated fetch, list or push.
func BasicAuth(token string) *http.BasicAuth {
	return &http.BasicAuth{
		Username: "du", // yes, this can be anything except an empty string
		Password: token,
	}
}

// open names the path in the error: it comes from the user or CI, so a bad one must say which.
func (rs *GitRepositoryService) open(path string) (*git.Repository, error) {
	checkout, err := git.PlainOpen(path)
	if err != nil {
		return nil, fmt.Errorf("git open %q: %w", path, err)
	}
	return checkout, nil
}

func NewGitRepositoryService(logger *zap.Logger) *GitRepositoryService {
	return &GitRepositoryService{
		logger: logger,
		fs:     afero.NewOsFs(),
	}
}

func (rs *GitRepositoryService) CloneRepository(repository string, branch string, token string, username string, email string) (Repository, Worktree, string, error) {

	h := fnv.New64a()
	_, _ = h.Write([]byte(repository))
	hash := fmt.Sprintf("%x", h.Sum64())
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
		Auth:          BasicAuth(token),
		Tags:          git.NoTags,
	})

	if err != nil {
		return nil, nil, "", fmt.Errorf("git clone: %w", err)
	}

	return rs.prepareCheckout(checkout, username, email)
}

// OpenRepository opens an existing checkout, with CloneRepository's git-user and hook setup.
func (rs *GitRepositoryService) OpenRepository(path string, username string, email string) (Repository, Worktree, string, error) {
	checkout, err := rs.open(path)
	if err != nil {
		return nil, nil, "", err
	}
	return rs.prepareCheckout(checkout, username, email)
}

// GetRemoteURL returns the "origin" URL, stripped of the credentials GitLab CI embeds in it.
func (rs *GitRepositoryService) GetRemoteURL(path string) (string, error) {
	checkout, err := rs.open(path)
	if err != nil {
		return "", err
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

// GetCurrentBranch returns HEAD's branch, or "" when detached — the usual CI state.
func (rs *GitRepositoryService) GetCurrentBranch(path string) (string, error) {
	checkout, err := rs.open(path)
	if err != nil {
		return "", err
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

// IsShallowClone reports a truncated history (CI's fetch-depth: 1). Such a checkout commits
// fine but fails the later push with "object not found".
func (rs *GitRepositoryService) IsShallowClone(path string) (bool, error) {
	checkout, err := rs.open(path)
	if err != nil {
		return false, err
	}
	shallow, err := checkout.Storer.Shallow()
	if err != nil {
		return false, fmt.Errorf("failed to read shallow commit info: %w", err)
	}
	return len(shallow) > 0, nil
}

// prepareCheckout sets the commit identity and removes the prepare-commit-msg hook.
func (rs *GitRepositoryService) prepareCheckout(checkout *git.Repository, username string, email string) (Repository, Worktree, string, error) {
	config, err := checkout.Config()
	if err != nil {
		return checkout, nil, "", fmt.Errorf("failed to read git config: %w", err)
	}
	// Empty values (no platform to ask) leave the checkout's own identity in place.
	if username != "" {
		config.User.Name = username
	}
	if email != "" {
		config.User.Email = email
	}

	// ...but a CI checkout has none at all, and go-git fails the first commit without one.
	// Checked against the scoped config go-git resolves from, so a global user.name still wins.
	scoped, err := checkout.ConfigScoped(gitconfig.SystemScope)
	if err != nil {
		return checkout, nil, "", fmt.Errorf("failed to read scoped git config: %w", err)
	}
	if config.User.Name == "" && scoped.User.Name == "" {
		config.User.Name = defaultCommitName
	}
	if config.User.Email == "" && scoped.User.Email == "" {
		config.User.Email = defaultCommitEmail
	}

	if err := checkout.SetConfig(config); err != nil {
		return checkout, nil, "", err
	}

	w, err := checkout.Worktree()
	if err != nil {
		return checkout, nil, "", err
	}
	root := w.Filesystem.Root()

	// `drush config:export --commit` shells out to real git, which does run hooks, and one that
	// prompts would wedge a run that cannot pass --no-verify. Through the OS filesystem, since
	// go-git's bound worktree rejects any path with a ".git" component.
	hookPath := filepath.Join(root, ".git", "hooks", "prepare-commit-msg")
	if _, err := rs.fs.Stat(hookPath); err == nil {
		if err := rs.fs.Remove(hookPath); err != nil {
			return checkout, w, "", fmt.Errorf("failed to remove prepare-commit-msg hook: %w", err)
		}
	}

	return checkout, w, root, nil
}

// BranchExists queries the remote: the cached refs/remotes/origin/* go stale the moment the host
// auto-deletes a merged branch, giving a false positive.
func (rs *GitRepositoryService) BranchExists(repository Repository, branch string, token string) (bool, error) {
	remote, err := repository.Remote("origin")
	if err != nil {
		return false, fmt.Errorf("failed to get origin remote: %w", err)
	}

	refs, err := remote.List(&git.ListOptions{Auth: BasicAuth(token)})
	if err != nil {
		return false, fmt.Errorf("failed to list remote refs: %w", err)
	}

	branchRef := plumbing.NewBranchReferenceName(branch).String()
	for _, ref := range refs {
		if ref.Name().String() == branchRef {
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
		if s.Staging != git.Unmodified && pathWithin(filePath, dir) {
			return true
		}
	}

	return false
}

// pathWithin reports whether filePath is dir or lives under it — not a substring test, which
// would match the sibling "translations-backup/de.po" for dir "translations". Empty dir matches all.
func pathWithin(filePath string, dir string) bool {
	dir = strings.Trim(filepath.ToSlash(dir), "/")
	if dir == "" {
		return true
	}
	filePath = strings.Trim(filepath.ToSlash(filePath), "/")
	return filePath == dir || strings.HasPrefix(filePath, dir+"/")
}
