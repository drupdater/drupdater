package repo

import (
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// isolateGitConfig gives these tests a CI runner's identity-less environment, not the
// developer's own ~/.gitconfig.
func isolateGitConfig(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	return home
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	require.NoError(t, err, string(out))
	return strings.TrimSpace(string(out))
}

// addWorktree returns a fresh source checkout on a branch and a worktree of it, removed again
// when the test ends.
func addWorktree(t *testing.T, username, email string) (string, Checkout) {
	t.Helper()
	source, _ := initRepoWithCommit(t)
	service := New(zap.NewNop())

	checkout, err := service.AddWorktree(t.Context(), source, Identity{Name: username, Email: email})
	require.NoError(t, err)
	t.Cleanup(func() { _ = service.RemoveWorktree(context.Background(), source, checkout.Path) })
	return source, checkout
}

func TestAddWorktreeIsADetachedCopyOfHead(t *testing.T) {
	isolateGitConfig(t)
	source, checkout := addWorktree(t, "Bot", "bot@example.com")

	assert.True(t, filepath.IsAbs(checkout.Path))
	assert.NotEqual(t, source, checkout.Path)
	assert.FileExists(t, filepath.Join(checkout.Path, ".git"), "a linked worktree has a .git file, not a directory")

	head, err := checkout.Repository.Head()
	require.NoError(t, err)
	assert.False(t, head.Name().IsBranch(), "detached, so no branch of the user's moves")
	assert.Equal(t, gitOutput(t, source, "rev-parse", "HEAD"), head.Hash().String())
	assert.Contains(t, gitOutput(t, source, "worktree", "list"), checkout.Path)
}

// Whatever the run commits, the user's branch, working tree and git config are as they were.
func TestCommitsInAWorktreeLeaveTheSourceCheckoutAlone(t *testing.T) {
	isolateGitConfig(t)
	source, checkout := addWorktree(t, "Bot", "bot@example.com")
	branchBefore := gitOutput(t, source, "rev-parse", "HEAD")
	configBefore, err := os.ReadFile(filepath.Join(source, ".git", "config"))
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(filepath.Join(checkout.Path, "update.txt"), []byte("new"), 0o600))
	_, err = checkout.Worktree.Add("update.txt")
	require.NoError(t, err)
	signature := checkout.Author.Signature()
	hash, err := checkout.Worktree.Commit("Update", &git.CommitOptions{Author: signature, Committer: signature})
	require.NoError(t, err)

	assert.Equal(t, branchBefore, gitOutput(t, source, "rev-parse", "HEAD"))
	assert.Empty(t, gitOutput(t, source, "status", "--porcelain"))
	assert.NoFileExists(t, filepath.Join(source, "update.txt"))
	configAfter, err := os.ReadFile(filepath.Join(source, ".git", "config"))
	require.NoError(t, err)
	assert.Equal(t, string(configBefore), string(configAfter))
	assert.Equal(t, "Bot <bot@example.com>", gitOutput(t, source, "log", "-1", "--format=%an <%ae>", hash.String()))
}

// The update branch is the one thing a run leaves in the user's repository, and only on purpose.
func TestSetAndRemoveBranchAreSeenByTheSourceCheckout(t *testing.T) {
	isolateGitConfig(t)
	source, checkout := addWorktree(t, "Bot", "bot@example.com")
	head, err := checkout.Repository.Head()
	require.NoError(t, err)

	require.NoError(t, checkout.Repository.SetBranch("update-abc", head.Hash()))
	assert.Equal(t, head.Hash().String(), gitOutput(t, source, "rev-parse", "refs/heads/update-abc"))
	ref, err := checkout.Repository.Reference(plumbing.NewBranchReferenceName("update-abc"), false)
	require.NoError(t, err)
	assert.Equal(t, head.Hash(), ref.Hash())

	require.NoError(t, checkout.Repository.RemoveBranch("update-abc"))
	_, err = checkout.Repository.Reference(plumbing.NewBranchReferenceName("update-abc"), false)
	require.ErrorIs(t, err, plumbing.ErrReferenceNotFound)
}

func TestRemoveWorktreeRemovesItsDirectoryAndRegistration(t *testing.T) {
	isolateGitConfig(t)
	source, _ := initRepoWithCommit(t)
	service := New(zap.NewNop())
	checkout, err := service.AddWorktree(t.Context(), source, Identity{})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(checkout.Path, "dirty.txt"), []byte("x"), 0o600))

	require.NoError(t, service.RemoveWorktree(t.Context(), source, checkout.Path))

	assert.NoDirExists(t, filepath.Dir(checkout.Path), "the temporary parent goes too")
	assert.NotContains(t, gitOutput(t, source, "worktree", "list"), checkout.Path)
}

// A directory already gone — removed by hand, or a killed run's — is pruned rather than an error.
func TestRemoveWorktreePrunesAWorktreeWhoseDirectoryIsGone(t *testing.T) {
	isolateGitConfig(t)
	source, _ := initRepoWithCommit(t)
	service := New(zap.NewNop())
	checkout, err := service.AddWorktree(t.Context(), source, Identity{})
	require.NoError(t, err)
	require.NoError(t, os.RemoveAll(filepath.Dir(checkout.Path)))

	require.NoError(t, service.RemoveWorktree(t.Context(), source, checkout.Path))

	assert.NotContains(t, gitOutput(t, source, "worktree", "list"), checkout.Path)
}

func TestRemoveWorktreeFailsWhenGitCannotRunAtAll(t *testing.T) {
	service := New(zap.NewNop())
	dir := t.TempDir()

	err := service.RemoveWorktree(t.Context(), filepath.Join(dir, "missing"), filepath.Join(dir, "x", "checkout"))

	require.ErrorContains(t, err, "failed to remove the worktree at")
}

func TestAddWorktreeErrors(t *testing.T) {
	isolateGitConfig(t)
	service := New(zap.NewNop())

	t.Run("not a repository", func(t *testing.T) {
		_, err := service.AddWorktree(t.Context(), t.TempDir(), Identity{})

		require.ErrorIs(t, err, git.ErrRepositoryNotExists)
		require.ErrorContains(t, err, "--working-dir must be the repository root")
	})

	t.Run("git refuses", func(t *testing.T) {
		// A repository without a commit has no HEAD to check out.
		dir := t.TempDir()
		_, err := git.PlainInit(dir, false)
		require.NoError(t, err)

		_, err = service.AddWorktree(t.Context(), dir, Identity{})

		require.ErrorContains(t, err, "failed to add a worktree of "+dir)
		matches, globErr := filepath.Glob(filepath.Join(os.TempDir(), "drupdater-worktree-*", "checkout"))
		require.NoError(t, globErr)
		for _, m := range matches {
			assert.NotContains(t, gitOutput(t, dir, "worktree", "list"), m)
		}
	})

	t.Run("git reports success but leaves no worktree", func(t *testing.T) {
		source, _ := initRepoWithCommit(t)
		var calls [][]string
		silent := &Git{logger: zap.NewNop(), newCommand: func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
			calls = append(calls, arg)
			return exec.CommandContext(ctx, "true")
		}}
		t.Setenv("TMPDIR", t.TempDir())

		_, err := silent.AddWorktree(t.Context(), source, Identity{})

		require.ErrorIs(t, err, git.ErrRepositoryNotExists)
		require.Len(t, calls, 2)
		assert.Contains(t, calls[1], "remove", "the half-made worktree must be removed again")
		matches, globErr := filepath.Glob(filepath.Join(os.TempDir(), "drupdater-worktree-*"))
		require.NoError(t, globErr)
		assert.Empty(t, matches)
	})

	t.Run("no temporary directory", func(t *testing.T) {
		source, _ := initRepoWithCommit(t)
		t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "missing"))

		_, err := service.AddWorktree(t.Context(), source, Identity{})

		require.ErrorContains(t, err, "failed to create temporary directory")
	})
}

// The platform's user, else the identity git config supplies, else drupdater's own — and in every
// case nothing is written into the repository's config.
func TestCommitIdentity(t *testing.T) {
	t.Run("the platform's user wins", func(t *testing.T) {
		home := isolateGitConfig(t)
		require.NoError(t, os.WriteFile(filepath.Join(home, ".gitconfig"),
			[]byte("[user]\n\tname = Global User\n\temail = global@example.com\n"), 0o600))

		_, checkout := addWorktree(t, "Bot", "bot@example.com")

		assert.Equal(t, Identity{Name: "Bot", Email: "bot@example.com"}, checkout.Author)
	})

	t.Run("git config supplies what the platform does not", func(t *testing.T) {
		home := isolateGitConfig(t)
		require.NoError(t, os.WriteFile(filepath.Join(home, ".gitconfig"),
			[]byte("[user]\n\tname = Global User\n\temail = global@example.com\n"), 0o600))

		_, checkout := addWorktree(t, "", "")

		assert.Equal(t, Identity{Name: "Global User", Email: "global@example.com"}, checkout.Author)
	})

	t.Run("each half is resolved on its own", func(t *testing.T) {
		home := isolateGitConfig(t)
		require.NoError(t, os.WriteFile(filepath.Join(home, ".gitconfig"),
			[]byte("[user]\n\temail = global@example.com\n"), 0o600))

		_, checkout := addWorktree(t, "Bot", "")

		assert.Equal(t, Identity{Name: "Bot", Email: "global@example.com"}, checkout.Author)
	})

	t.Run("drupdater's own when nothing supplies one", func(t *testing.T) {
		isolateGitConfig(t)

		source, checkout := addWorktree(t, "", "")

		assert.Equal(t, Identity{Name: defaultCommitName, Email: defaultCommitEmail}, checkout.Author)
		config, err := os.ReadFile(filepath.Join(source, ".git", "config"))
		require.NoError(t, err)
		assert.NotContains(t, string(config), "[user]")
	})
}

func TestSignatureIsTheIdentityNow(t *testing.T) {
	signature := Identity{Name: "Bot", Email: "bot@example.com"}.Signature()

	assert.Equal(t, "Bot", signature.Name)
	assert.Equal(t, "bot@example.com", signature.Email)
	assert.False(t, signature.When.IsZero(), "a zero time would date every commit 0001-01-01")
}

func TestGitCommandsTrustTheRepository(t *testing.T) {
	var args []string
	rs := &Git{newCommand: func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
		args = arg
		return exec.CommandContext(ctx, "true")
	}}

	_, err := rs.runGit(t.Context(), t.TempDir(), "worktree", "prune")

	require.NoError(t, err)
	assert.Equal(t, []string{"-c", "safe.directory=*", "worktree", "prune"}, args)
}

// The container runs as root against a checkout the host user owns. Only root can create that
// situation, so elsewhere the argument test above stands in for it.
func TestAddWorktreeOfARepositoryOwnedByAnotherUser(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("needs root to hand the repository to another user")
	}
	isolateGitConfig(t)
	source, _ := initRepoWithCommit(t)
	root, err := os.OpenRoot(source)
	require.NoError(t, err)
	t.Cleanup(func() { _ = root.Close() })
	require.NoError(t, fs.WalkDir(root.FS(), ".", func(path string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return root.Lchown(path, 65534, 65534)
	}))
	service := New(zap.NewNop())

	checkout, err := service.AddWorktree(t.Context(), source, Identity{})
	require.NoError(t, err)
	require.NoError(t, service.RemoveWorktree(t.Context(), source, checkout.Path))
}
