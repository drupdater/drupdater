package repo

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/afero"

	git "github.com/go-git/go-git/v5"
	gitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
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
	// SetBranch points the local branch name at hash, creating it when absent.
	SetBranch(name string, hash plumbing.Hash) error
	// RemoveBranch deletes the local branch name.
	RemoveBranch(name string) error
}

type Worktree interface {
	Add(path string) (plumbing.Hash, error)
	AddGlob(pattern string) error
	Remove(path string) (plumbing.Hash, error)
	Commit(msg string, opts *git.CommitOptions) (plumbing.Hash, error)
	Status() (git.Status, error)
}

// Identity is who drupdater's commits are by.
type Identity struct {
	Name  string
	Email string
}

// Signature is the identity at the current time, which is what a go-git commit records.
func (i Identity) Signature() *object.Signature {
	return &object.Signature{Name: i.Name, Email: i.Email, When: time.Now()}
}

// Checkout is a working copy drupdater changes and commits in: its own clone, or a linked worktree
// of the user's checkout. Commits carry Author explicitly, so the repository's git config is never
// written to.
type Checkout struct {
	Repository Repository
	Worktree   Worktree
	Path       string
	Author     Identity
}

type repository struct{ *git.Repository }

func (r repository) SetBranch(name string, hash plumbing.Hash) error {
	return r.Storer.SetReference(plumbing.NewHashReference(plumbing.NewBranchReferenceName(name), hash))
}

func (r repository) RemoveBranch(name string) error {
	return r.Storer.RemoveReference(plumbing.NewBranchReferenceName(name))
}

type Git struct {
	logger *zap.Logger
	fs     afero.Fs
	// newCommand runs git for what go-git cannot do: linked worktrees.
	newCommand func(ctx context.Context, name string, args ...string) *exec.Cmd
}

// BasicAuth is the credential go-git needs for an authenticated fetch, list or push.
func BasicAuth(token string) *http.BasicAuth {
	return &http.BasicAuth{
		Username: "du", // yes, this can be anything except an empty string
		Password: token,
	}
}

// open names the path in the error: it comes from the user or CI, so a bad one must say which.
func (rs *Git) open(path string) (*git.Repository, error) {
	// The common dir is where a linked worktree keeps its refs, remotes and shallow file.
	checkout, err := git.PlainOpenWithOptions(path, &git.PlainOpenOptions{EnableDotGitCommonDir: true})
	if err != nil {
		if errors.Is(err, git.ErrRepositoryNotExists) {
			return nil, fmt.Errorf("no git repository at %q: --working-dir must be the repository root (the directory containing .git): %w", path, err)
		}
		return nil, fmt.Errorf("git open %q: %w", path, err)
	}
	return checkout, nil
}

// New returns a Git that runs on the host.
func New(logger *zap.Logger) *Git {
	return &Git{
		logger:     logger,
		fs:         afero.NewOsFs(),
		newCommand: exec.CommandContext,
	}
}

// CloneRepository clones the branch into a new temporary directory.
func (rs *Git) CloneRepository(repository string, branch string, token string, requested Identity) (Checkout, error) {

	h := fnv.New64a()
	_, _ = h.Write([]byte(repository))
	hash := fmt.Sprintf("%x", h.Sum64())
	projectDir := filepath.Join(os.TempDir(), hash)
	if err := rs.fs.MkdirAll(projectDir, os.ModePerm); err != nil {
		return Checkout{}, fmt.Errorf("failed to create project directory: %w", err)
	}
	tmpDirName, err := afero.TempDir(rs.fs, projectDir, "repo")
	if err != nil {
		return Checkout{}, fmt.Errorf("failed to create temporary directory: %w", err)
	}

	checkout, err := git.PlainClone(tmpDirName, false, &git.CloneOptions{
		URL:           repository,
		Depth:         1,
		ReferenceName: plumbing.NewBranchReferenceName(branch),
		Auth:          BasicAuth(token),
		Tags:          git.NoTags,
	})

	if err != nil {
		return Checkout{}, fmt.Errorf("git clone: %w", err)
	}

	return rs.checkout(checkout, requested)
}

// AddWorktree adds a linked worktree of the checkout at source, detached at its HEAD, in a new
// temporary directory. The run works and commits there, so the user's working tree, branch and
// config stay as they were whatever happens. Through the git CLI: go-git cannot create one.
func (rs *Git) AddWorktree(ctx context.Context, source string, requested Identity) (Checkout, error) {
	if _, err := rs.open(source); err != nil {
		return Checkout{}, err
	}

	parent, err := os.MkdirTemp("", "drupdater-worktree-")
	if err != nil {
		return Checkout{}, fmt.Errorf("failed to create temporary directory: %w", err)
	}
	path := filepath.Join(parent, "checkout")
	if out, err := rs.runGit(ctx, source, "worktree", "add", "--detach", path, "HEAD"); err != nil {
		_ = os.RemoveAll(parent)
		return Checkout{}, fmt.Errorf("failed to add a worktree of %s: %w: %s", source, err, out)
	}

	checkout, err := rs.open(path)
	if err != nil {
		_ = rs.RemoveWorktree(ctx, source, path)
		return Checkout{}, err
	}
	return rs.checkout(checkout, requested)
}

// RemoveWorktree removes a worktree AddWorktree added, its directory included.
func (rs *Git) RemoveWorktree(ctx context.Context, source string, path string) error {
	out, err := rs.runGit(ctx, source, "worktree", "remove", "--force", path)
	// The temporary parent goes either way; after a failed remove, prune drops the registration.
	if removeErr := os.RemoveAll(filepath.Dir(path)); removeErr != nil && err == nil {
		return fmt.Errorf("failed to remove the worktree directory: %w", removeErr)
	}
	if err != nil {
		if _, pruneErr := rs.runGit(ctx, source, "worktree", "prune"); pruneErr != nil {
			return fmt.Errorf("failed to remove the worktree at %s: %w: %s", path, err, out)
		}
	}
	return nil
}

// runGit trusts the repository for this one command: a checkout mounted into the container is
// owned by another user, which git refuses as "dubious ownership" — and go-git never checks.
func (rs *Git) runGit(ctx context.Context, dir string, args ...string) (string, error) {
	command := rs.newCommand(ctx, "git", append([]string{"-c", "safe.directory=*"}, args...)...)
	command.Dir = dir
	out, err := command.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// GetRemoteURL returns the "origin" URL, stripped of the credentials GitLab CI embeds in it.
func (rs *Git) GetRemoteURL(path string) (string, error) {
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
func (rs *Git) GetCurrentBranch(path string) (string, error) {
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
func (rs *Git) IsShallowClone(path string) (bool, error) {
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

// checkout pairs a repository with its worktree and the identity its commits are by: the
// requested one, else the one git config already supplies, else drupdater's own.
func (rs *Git) checkout(checkout *git.Repository, requested Identity) (Checkout, error) {
	scoped, err := checkout.ConfigScoped(gitconfig.SystemScope)
	if err != nil {
		return Checkout{}, fmt.Errorf("failed to read git config: %w", err)
	}
	author := requested
	if author.Name == "" {
		author.Name = cmp.Or(scoped.User.Name, defaultCommitName)
	}
	if author.Email == "" {
		author.Email = cmp.Or(scoped.User.Email, defaultCommitEmail)
	}

	w, err := checkout.Worktree()
	if err != nil {
		return Checkout{}, err
	}

	return Checkout{
		Repository: repository{checkout},
		Worktree:   w,
		Path:       w.Filesystem.Root(),
		Author:     author,
	}, nil
}

// BranchExists queries the remote: the cached refs/remotes/origin/* go stale the moment the host
// auto-deletes a merged branch, giving a false positive.
func (rs *Git) BranchExists(repository Repository, branch string, token string) (bool, error) {
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
