package repo

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/afero"
)

// Last-resort commit identity, when neither the platform nor the checkout supplies one.
const (
	defaultCommitName  = "drupdater"
	defaultCommitEmail = "drupdater@localhost"
)

// ErrNotARepository is returned for a path that is not the root of a git repository.
var ErrNotARepository = errors.New("not a git repository")

// Repository is the refs and remote of a working copy's repository.
type Repository interface {
	// Head is the commit the working copy is at.
	Head(ctx context.Context) (string, error)
	// BranchExists reports whether the local branch name exists.
	BranchExists(ctx context.Context, name string) (bool, error)
	// SetBranch points the local branch name at commit, creating it when absent.
	SetBranch(ctx context.Context, name string, commit string) error
	// RemoveBranch deletes the local branch name.
	RemoveBranch(ctx context.Context, name string) error
	// Push pushes the local branch name to the same name on origin.
	Push(ctx context.Context, name string, token string) error
	// RemoteBranchExists reports whether origin has the branch name.
	RemoteBranchExists(ctx context.Context, name string, token string) (bool, error)
}

// Worktree is the files of a working copy, and the commits made from them.
type Worktree interface {
	// Stage stages paths, files or directories, deletions included. A path that neither exists
	// nor is tracked, or that git ignores, has nothing to stage and is skipped.
	Stage(ctx context.Context, paths ...string) error
	// StageGlob stages the files matching pattern, relative to the working copy's root.
	StageGlob(ctx context.Context, pattern string) error
	// Unstaged lists the tracked files changed in the working tree but not staged.
	Unstaged(ctx context.Context) ([]string, error)
	// Staged lists the files staged for the next commit.
	Staged(ctx context.Context) ([]string, error)
	// Commit commits what is staged, by author.
	Commit(ctx context.Context, message string, author Identity) error
}

// Identity is who drupdater's commits are by.
type Identity struct {
	Name  string
	Email string
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

// Git runs the git CLI.
type Git struct {
	fs         afero.Fs
	newCommand func(ctx context.Context, name string, args ...string) *exec.Cmd
}

// New returns a Git that runs on the host.
func New() *Git {
	return &Git{
		fs:         afero.NewOsFs(),
		newCommand: exec.CommandContext,
	}
}

// commonArgs keep a run independent of the git config of whoever owns the checkout: a checkout
// mounted into the container is owned by another user, which git refuses as "dubious ownership";
// the project's hooks and signing setup are for its developers' commits, not drupdater's.
var commonArgs = []string{
	"-c", "safe.directory=*",
	"-c", "core.hooksPath=/dev/null",
	"-c", "commit.gpgSign=false",
}

// tokenEnv carries the token to credentialHelper, so it is in neither argv nor any config file.
const tokenEnv = "DRUPDATER_GIT_TOKEN" //nolint:gosec // the variable's name, not a credential

// credentialHelper answers git's credential requests with the token. Any username will do, but
// not an empty one.
const credentialHelper = `!f() { test "$1" = get && printf 'username=du\npassword=%s\n' "$` + tokenEnv + `"; }; f`

// command builds git args run in dir. With a token, the user's own credential helpers are
// replaced by one that answers with it.
func (rs *Git) command(ctx context.Context, dir string, token string, args ...string) *exec.Cmd {
	all := slices.Clone(commonArgs)
	if token != "" {
		all = append(all, "-c", "credential.helper=", "-c", "credential.helper="+credentialHelper)
	}
	cmd := rs.newCommand(ctx, "git", append(all, args...)...)
	cmd.Dir = dir
	// Unanswered, a credential prompt would hang a CI job until its timeout.
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if token != "" {
		cmd.Env = append(cmd.Env, tokenEnv+"="+token)
	}
	return cmd
}

func (rs *Git) run(ctx context.Context, dir string, args ...string) (string, error) {
	return rs.runAuthenticated(ctx, dir, "", args...)
}

// runAuthenticated returns stdout, and an error carrying stderr.
func (rs *Git) runAuthenticated(ctx context.Context, dir string, token string, args ...string) (string, error) {
	cmd := rs.command(ctx, dir, token, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return stdout.String(), fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// exitedWith reports whether git ran to completion and exited with code.
func exitedWith(err error, code int) bool {
	exitErr, ok := errors.AsType[*exec.ExitError](err)
	return ok && exitErr.ExitCode() == code
}

// open checks that path is the root of a repository. It names the path in the error: it comes
// from the user or CI, so a bad one must say which.
func (rs *Git) open(ctx context.Context, path string) error {
	notRoot := fmt.Errorf("no git repository at %q: --working-dir must be the repository root (the directory containing .git): %w", path, ErrNotARepository)
	out, err := rs.run(ctx, path, "rev-parse", "--show-toplevel")
	if err != nil {
		if _, statErr := os.Stat(path); statErr != nil {
			return fmt.Errorf("%w: %w", notRoot, statErr)
		}
		return notRoot
	}
	if !samePath(strings.TrimSpace(out), path) {
		return notRoot
	}
	return nil
}

// samePath compares the directories themselves: a temporary directory is often reached through a
// symlink, and a working directory may be given relative.
func samePath(a, b string) bool {
	infoA, errA := os.Stat(a)
	infoB, errB := os.Stat(b)
	return errA == nil && errB == nil && os.SameFile(infoA, infoB)
}

// CloneRepository clones the branch into a new temporary directory.
func (rs *Git) CloneRepository(ctx context.Context, repository string, branch string, token string, requested Identity) (Checkout, error) {
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

	if _, err := rs.runAuthenticated(ctx, projectDir, token,
		"clone", "--depth", "1", "--branch", branch, "--single-branch", "--no-tags", "--", repository, tmpDirName); err != nil {
		return Checkout{}, err
	}

	return rs.checkout(ctx, tmpDirName, requested), nil
}

// AddWorktree adds a linked worktree of the checkout at source, detached at its HEAD, in a new
// temporary directory. The run works and commits there, so the user's working tree, branch and
// config stay as they were whatever happens.
func (rs *Git) AddWorktree(ctx context.Context, source string, requested Identity) (Checkout, error) {
	if err := rs.open(ctx, source); err != nil {
		return Checkout{}, err
	}

	parent, err := os.MkdirTemp("", "drupdater-worktree-")
	if err != nil {
		return Checkout{}, fmt.Errorf("failed to create temporary directory: %w", err)
	}
	path := filepath.Join(parent, "checkout")
	if _, err := rs.run(ctx, source, "worktree", "add", "--detach", path, "HEAD"); err != nil {
		_ = os.RemoveAll(parent)
		return Checkout{}, fmt.Errorf("failed to add a worktree of %s: %w", source, err)
	}

	if err := rs.open(ctx, path); err != nil {
		_ = rs.RemoveWorktree(ctx, source, path)
		return Checkout{}, err
	}
	return rs.checkout(ctx, path, requested), nil
}

// RemoveWorktree removes a worktree AddWorktree added, its directory included.
func (rs *Git) RemoveWorktree(ctx context.Context, source string, path string) error {
	_, err := rs.run(ctx, source, "worktree", "remove", "--force", path)
	// The temporary parent goes either way; after a failed remove, prune drops the registration.
	if removeErr := os.RemoveAll(filepath.Dir(path)); removeErr != nil && err == nil {
		return fmt.Errorf("failed to remove the worktree directory: %w", removeErr)
	}
	if err != nil {
		if _, pruneErr := rs.run(ctx, source, "worktree", "prune"); pruneErr != nil {
			return fmt.Errorf("failed to remove the worktree at %s: %w", path, err)
		}
	}
	return nil
}

// GetRemoteURL returns the "origin" URL, stripped of the credentials GitLab CI embeds in it.
func (rs *Git) GetRemoteURL(ctx context.Context, path string) (string, error) {
	if err := rs.open(ctx, path); err != nil {
		return "", err
	}
	return rs.originURL(ctx, path)
}

// originURL reads the URL as configured, before any insteadOf rewriting a user set up for their
// own pushes.
func (rs *Git) originURL(ctx context.Context, dir string) (string, error) {
	out, err := rs.run(ctx, dir, "config", "--get", "remote.origin.url")
	if err != nil {
		return "", fmt.Errorf("failed to get origin remote: %w", err)
	}
	raw := strings.TrimSpace(out)
	if raw == "" {
		return "", errors.New("origin remote has no URL")
	}
	if u, err := url.Parse(raw); err == nil {
		u.User = nil
		return u.String(), nil
	}
	return raw, nil
}

// GetCurrentBranch returns HEAD's branch, or "" when detached — the usual CI state.
func (rs *Git) GetCurrentBranch(ctx context.Context, path string) (string, error) {
	if err := rs.open(ctx, path); err != nil {
		return "", err
	}
	out, err := rs.run(ctx, path, "symbolic-ref", "--quiet", "--short", "HEAD")
	if exitedWith(err, 1) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("failed to get HEAD: %w", err)
	}
	return strings.TrimSpace(out), nil
}

// IsShallowClone reports a truncated history (CI's fetch-depth: 1). Such a checkout commits
// fine but fails the later push with "object not found".
func (rs *Git) IsShallowClone(ctx context.Context, path string) (bool, error) {
	if err := rs.open(ctx, path); err != nil {
		return false, err
	}
	out, err := rs.run(ctx, path, "rev-parse", "--is-shallow-repository")
	if err != nil {
		return false, fmt.Errorf("failed to read shallow commit info: %w", err)
	}
	return strings.TrimSpace(out) == "true", nil
}

// checkout pairs the working copy at path with the identity its commits are by: the requested
// one, else the one git config already supplies, else drupdater's own.
func (rs *Git) checkout(ctx context.Context, path string, requested Identity) Checkout {
	author := requested
	if author.Name == "" {
		author.Name = cmp.Or(rs.configValue(ctx, path, "user.name"), defaultCommitName)
	}
	if author.Email == "" {
		author.Email = cmp.Or(rs.configValue(ctx, path, "user.email"), defaultCommitEmail)
	}

	wc := &workingCopy{git: rs, dir: path}
	return Checkout{Repository: wc, Worktree: wc, Path: path, Author: author}
}

// configValue is "" when key is unset: git then exits 1 and prints nothing.
func (rs *Git) configValue(ctx context.Context, dir string, key string) string {
	out, _ := rs.run(ctx, dir, "config", "--get", key)
	return strings.TrimSpace(out)
}

// workingCopy is a Repository and its Worktree, both run through the git CLI in dir.
type workingCopy struct {
	git *Git
	dir string
}

func (w *workingCopy) Head(ctx context.Context) (string, error) {
	out, err := w.git.run(ctx, w.dir, "rev-parse", "--verify", "HEAD")
	return strings.TrimSpace(out), err
}

func (w *workingCopy) BranchExists(ctx context.Context, name string) (bool, error) {
	_, err := w.git.run(ctx, w.dir, "show-ref", "--verify", "--quiet", "refs/heads/"+name)
	if exitedWith(err, 1) {
		return false, nil
	}
	return err == nil, err
}

func (w *workingCopy) SetBranch(ctx context.Context, name string, commit string) error {
	_, err := w.git.run(ctx, w.dir, "update-ref", "refs/heads/"+name, commit)
	return err
}

func (w *workingCopy) RemoveBranch(ctx context.Context, name string) error {
	_, err := w.git.run(ctx, w.dir, "update-ref", "-d", "refs/heads/"+name)
	return err
}

// Push goes to origin's URL stripped of any credentials in it, so the token decides: GitLab CI
// embeds a job token there that may not push.
func (w *workingCopy) Push(ctx context.Context, name string, token string) error {
	origin, err := w.git.originURL(ctx, w.dir)
	if err != nil {
		return err
	}
	ref := "refs/heads/" + name
	_, err = w.git.runAuthenticated(ctx, w.dir, token, "push", "--no-verify", origin, ref+":"+ref)
	return err
}

// RemoteBranchExists queries the remote: the cached refs/remotes/origin/* go stale the moment the
// host auto-deletes a merged branch, giving a false positive.
func (w *workingCopy) RemoteBranchExists(ctx context.Context, name string, token string) (bool, error) {
	origin, err := w.git.originURL(ctx, w.dir)
	if err != nil {
		return false, err
	}
	out, err := w.git.runAuthenticated(ctx, w.dir, token, "ls-remote", "--heads", origin, "refs/heads/"+name)
	if err != nil {
		return false, fmt.Errorf("failed to list remote refs: %w", err)
	}
	for line := range strings.SplitSeq(out, "\n") {
		if _, ref, ok := strings.Cut(strings.TrimSpace(line), "\t"); ok && ref == "refs/heads/"+name {
			return true, nil
		}
	}
	return false, nil
}

func (w *workingCopy) Stage(ctx context.Context, paths ...string) error {
	stageable, err := w.stageable(ctx, paths)
	if err != nil {
		return err
	}
	// With no pathspec at all, git add --all would stage the whole tree.
	if len(stageable) == 0 {
		return nil
	}
	_, err = w.git.run(ctx, w.dir, append([]string{"add", "--all", "--"}, literal(stageable)...)...)
	return err
}

// stageable drops the paths git add would refuse: neither on disk nor tracked, or ignored.
func (w *workingCopy) stageable(ctx context.Context, paths []string) ([]string, error) {
	var known []string
	for _, p := range paths {
		if _, err := os.Lstat(filepath.Join(w.dir, p)); err == nil {
			known = append(known, p)
			continue
		}
		if _, err := w.git.run(ctx, w.dir, "ls-files", "--error-unmatch", "--", ":(literal)"+p); err == nil {
			known = append(known, p)
		}
	}
	if len(known) == 0 {
		return nil, nil
	}
	// Without --no-index a tracked file is never reported, which is what git add does too.
	cmd := w.git.command(ctx, w.dir, "", "check-ignore", "--stdin", "-z")
	cmd.Stdin = strings.NewReader(strings.Join(known, "\x00") + "\x00")
	out, err := cmd.Output()
	if err != nil && !exitedWith(err, 1) {
		return nil, fmt.Errorf("failed to read which paths git ignores: %w", err)
	}
	ignored := splitZ(string(out))
	return slices.DeleteFunc(known, func(p string) bool { return slices.Contains(ignored, p) }), nil
}

func (w *workingCopy) StageGlob(ctx context.Context, pattern string) error {
	_, err := w.git.run(ctx, w.dir, "add", "--all", "--", ":(glob)"+pattern)
	return err
}

func (w *workingCopy) Unstaged(ctx context.Context) ([]string, error) {
	out, err := w.git.run(ctx, w.dir, "diff", "--name-only", "-z")
	return splitZ(out), err
}

func (w *workingCopy) Staged(ctx context.Context) ([]string, error) {
	out, err := w.git.run(ctx, w.dir, "diff", "--cached", "--name-only", "-z")
	return splitZ(out), err
}

func (w *workingCopy) Commit(ctx context.Context, message string, author Identity) error {
	cmd := w.git.command(ctx, w.dir, "", "commit", "--quiet", "--no-verify", "--cleanup=verbatim", "--file", "-")
	cmd.Env = append(cmd.Env,
		"GIT_AUTHOR_NAME="+author.Name, "GIT_AUTHOR_EMAIL="+author.Email,
		"GIT_COMMITTER_NAME="+author.Name, "GIT_COMMITTER_EMAIL="+author.Email,
	)
	cmd.Stdin = strings.NewReader(message)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git commit: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func literal(paths []string) []string {
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = ":(literal)" + p
	}
	return out
}

func splitZ(out string) []string {
	return slices.DeleteFunc(strings.Split(out, "\x00"), func(s string) bool { return s == "" })
}
