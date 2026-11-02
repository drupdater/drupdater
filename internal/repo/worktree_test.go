package repo

import (
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// addWorktree returns a fresh source checkout on main and a worktree of it, removed again when
// the test ends.
func addWorktree(t *testing.T, username, email string) (string, Checkout) {
	t.Helper()
	source := initRepoWithCommit(t)
	service := New()

	checkout, err := service.AddWorktree(t.Context(), source, Identity{Name: username, Email: email})
	require.NoError(t, err)
	t.Cleanup(func() { _ = service.RemoveWorktree(t.Context(), source, checkout.Path) })
	return source, checkout
}

func TestAddWorktreeIsADetachedCopyOfHead(t *testing.T) {
	isolateGitConfig(t)
	source, checkout := addWorktree(t, "Bot", "bot@example.com")

	assert.True(t, filepath.IsAbs(checkout.Path))
	assert.NotEqual(t, source, checkout.Path)
	assert.FileExists(t, filepath.Join(checkout.Path, ".git"), "a linked worktree has a .git file, not a directory")

	head, err := checkout.Repository.Head(t.Context())
	require.NoError(t, err)
	assert.Equal(t, gitOutput(t, source, "rev-parse", "HEAD"), head)
	assert.Equal(t, "HEAD", gitOutput(t, checkout.Path, "rev-parse", "--abbrev-ref", "HEAD"),
		"detached, so no branch of the user's moves")
	assert.Contains(t, gitOutput(t, source, "worktree", "list"), checkout.Path)
}

// Whatever the run commits, the user's branch, working tree and git config are as they were.
func TestCommitsInAWorktreeLeaveTheSourceCheckoutAlone(t *testing.T) {
	isolateGitConfig(t)
	source, checkout := addWorktree(t, "Bot", "bot@example.com")
	branchBefore := gitOutput(t, source, "rev-parse", "HEAD")
	configBefore, err := os.ReadFile(filepath.Join(source, ".git", "config"))
	require.NoError(t, err)

	writeFile(t, checkout.Path, "update.txt", "new")
	require.NoError(t, checkout.Worktree.Stage(t.Context(), "update.txt"))
	require.NoError(t, checkout.Worktree.Commit(t.Context(), "Update", checkout.Author))

	assert.Equal(t, branchBefore, gitOutput(t, source, "rev-parse", "HEAD"))
	assert.Empty(t, gitOutput(t, source, "status", "--porcelain"))
	assert.NoFileExists(t, filepath.Join(source, "update.txt"))
	configAfter, err := os.ReadFile(filepath.Join(source, ".git", "config"))
	require.NoError(t, err)
	assert.Equal(t, string(configBefore), string(configAfter))
	assert.Equal(t, "Bot <bot@example.com> Bot <bot@example.com>",
		gitOutput(t, checkout.Path, "log", "-1", "--format=%an <%ae> %cn <%ce>"))
}

// The project's hooks and signing setup are its developers', and would block or alter a run's
// commits: a failing hook, or a signing key the CI runner does not have.
func TestCommitAndPushIgnoreTheProjectsHooksAndSigning(t *testing.T) {
	origin, checkout := cloneOf(t)
	hooks := filepath.Join(checkout.Path, ".git", "hooks")
	for _, hook := range []string{"pre-commit", "commit-msg", "pre-push"} {
		require.NoError(t, os.WriteFile(filepath.Join(hooks, hook), []byte("#!/bin/sh\nexit 1\n"), 0o755)) //nolint:gosec // a hook must be executable
	}
	gitOutput(t, checkout.Path, "config", "commit.gpgSign", "true")
	gitOutput(t, checkout.Path, "config", "gpg.program", "false")

	writeFile(t, checkout.Path, "update.txt", "new")
	require.NoError(t, checkout.Worktree.Stage(t.Context(), "update.txt"))
	require.NoError(t, checkout.Worktree.Commit(t.Context(), "Update", checkout.Author))
	head, err := checkout.Repository.Head(t.Context())
	require.NoError(t, err)
	require.NoError(t, checkout.Repository.SetBranch(t.Context(), "update-abc", head))
	require.NoError(t, checkout.Repository.Push(t.Context(), "update-abc", ""))

	assert.Equal(t, head, gitOutput(t, origin, "rev-parse", "refs/heads/update-abc"))
}

func TestCommitKeepsTheMessageVerbatim(t *testing.T) {
	isolateGitConfig(t)
	_, checkout := addWorktree(t, "Bot", "bot@example.com")
	writeFile(t, checkout.Path, "a.txt", "a")
	require.NoError(t, checkout.Worktree.Stage(t.Context(), "a.txt"))

	require.NoError(t, checkout.Worktree.Commit(t.Context(), "Update #123\n\n# not a comment", checkout.Author))

	assert.Equal(t, "Update #123\n\n# not a comment", gitOutput(t, checkout.Path, "log", "-1", "--format=%B"))
}

func TestCommitFailsWithNothingStaged(t *testing.T) {
	isolateGitConfig(t)
	_, checkout := addWorktree(t, "Bot", "bot@example.com")

	err := checkout.Worktree.Commit(t.Context(), "Nothing", checkout.Author)

	require.ErrorContains(t, err, "git commit")
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr, "git's own failure survives the wrapper")
}

// A failed git command is an error, whatever it printed, and keeps git's own failure in it.
func TestFailedGitCommandsAreErrors(t *testing.T) {
	isolateGitConfig(t)
	empty := &workingCopy{git: New(), dir: initRepo(t)}
	var exitErr *exec.ExitError

	_, err := empty.Head(t.Context())
	require.ErrorAs(t, err, &exitErr, "a repository without a commit has no HEAD")

	require.ErrorAs(t, empty.SetBranch(t.Context(), "update-abc", "not-a-commit"), &exitErr)
	require.ErrorAs(t, empty.RemoveBranch(t.Context(), "no/such//branch"), &exitErr)
	require.ErrorAs(t, empty.StageGlob(t.Context(), "nothing-matches-*"), &exitErr)
}

// The update branch is the one thing a run leaves in the user's repository, and only on purpose.
func TestSetAndRemoveBranchAreSeenByTheSourceCheckout(t *testing.T) {
	isolateGitConfig(t)
	source, checkout := addWorktree(t, "Bot", "bot@example.com")
	head, err := checkout.Repository.Head(t.Context())
	require.NoError(t, err)

	require.NoError(t, checkout.Repository.SetBranch(t.Context(), "update-abc", head))
	assert.Equal(t, head, gitOutput(t, source, "rev-parse", "refs/heads/update-abc"))
	exists, err := checkout.Repository.BranchExists(t.Context(), "update-abc")
	require.NoError(t, err)
	assert.True(t, exists)

	require.NoError(t, checkout.Repository.RemoveBranch(t.Context(), "update-abc"))
	exists, err = checkout.Repository.BranchExists(t.Context(), "update-abc")
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestBranchExistsFailsOutsideARepository(t *testing.T) {
	wc := &workingCopy{git: New(), dir: filepath.Join(t.TempDir(), "missing")}

	_, err := wc.BranchExists(t.Context(), "main")

	require.Error(t, err)
}

func TestStage(t *testing.T) {
	setup := func(t *testing.T) Checkout {
		t.Helper()
		isolateGitConfig(t)
		source := initRepo(t)
		writeFile(t, source, ".gitignore", "ignored/\n")
		writeFile(t, source, "tracked.txt", "a")
		writeFile(t, source, "dir/kept.txt", "a")
		writeFile(t, source, "dir/gone.txt", "a")
		gitOutput(t, source, "add", ".")
		gitOutput(t, source, "commit", "--quiet", "-m", "init")
		checkout, err := New().AddWorktree(t.Context(), source, Identity{})
		require.NoError(t, err)
		t.Cleanup(func() { _ = New().RemoveWorktree(t.Context(), source, checkout.Path) })
		return checkout
	}

	t.Run("a directory, deletions included", func(t *testing.T) {
		checkout := setup(t)
		writeFile(t, checkout.Path, "dir/kept.txt", "b")
		writeFile(t, checkout.Path, "dir/new.txt", "b")
		require.NoError(t, os.Remove(filepath.Join(checkout.Path, "dir", "gone.txt")))

		require.NoError(t, checkout.Worktree.Stage(t.Context(), "dir"))

		staged, err := checkout.Worktree.Staged(t.Context())
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"dir/gone.txt", "dir/kept.txt", "dir/new.txt"}, staged)
	})

	t.Run("a deleted file named on its own", func(t *testing.T) {
		checkout := setup(t)
		require.NoError(t, os.Remove(filepath.Join(checkout.Path, "tracked.txt")))

		require.NoError(t, checkout.Worktree.Stage(t.Context(), "tracked.txt"))

		staged, err := checkout.Worktree.Staged(t.Context())
		require.NoError(t, err)
		assert.Equal(t, []string{"tracked.txt"}, staged)
	})

	t.Run("skips a path that neither exists nor is tracked, and an ignored one", func(t *testing.T) {
		checkout := setup(t)
		writeFile(t, checkout.Path, "ignored/x.txt", "b")
		writeFile(t, checkout.Path, "tracked.txt", "b")

		require.NoError(t, checkout.Worktree.Stage(t.Context(), "missing", "ignored/x.txt", "tracked.txt"))

		staged, err := checkout.Worktree.Staged(t.Context())
		require.NoError(t, err)
		assert.Equal(t, []string{"tracked.txt"}, staged)
	})

	t.Run("only skipped paths stage nothing", func(t *testing.T) {
		// Changes everywhere else, which a git add with no paths left would sweep up.
		checkout := setup(t)
		writeFile(t, checkout.Path, "tracked.txt", "b")
		writeFile(t, checkout.Path, "untracked.txt", "b")

		require.NoError(t, checkout.Worktree.Stage(t.Context(), "missing"))
		require.NoError(t, checkout.Worktree.Stage(t.Context()))

		staged, err := checkout.Worktree.Staged(t.Context())
		require.NoError(t, err)
		assert.Empty(t, staged)
	})

	t.Run("a path is literal, never a pattern", func(t *testing.T) {
		checkout := setup(t)
		writeFile(t, checkout.Path, "tracked.txt", "b")
		writeFile(t, checkout.Path, "t*", "b")

		require.NoError(t, checkout.Worktree.Stage(t.Context(), "t*"))

		staged, err := checkout.Worktree.Staged(t.Context())
		require.NoError(t, err)
		assert.Equal(t, []string{"t*"}, staged)
	})

	t.Run("fails outside a repository", func(t *testing.T) {
		wc := &workingCopy{git: New(), dir: t.TempDir()}
		writeFile(t, wc.dir, "a.txt", "a")

		err := wc.Stage(t.Context(), "a.txt")
		require.ErrorContains(t, err, "failed to read which paths git ignores")
		var exitErr *exec.ExitError
		require.ErrorAs(t, err, &exitErr)
	})

	t.Run("Unstaged lists only tracked files with unstaged changes", func(t *testing.T) {
		checkout := setup(t)
		writeFile(t, checkout.Path, "tracked.txt", "b")
		writeFile(t, checkout.Path, "dir/kept.txt", "b")
		writeFile(t, checkout.Path, "untracked.txt", "b")
		require.NoError(t, checkout.Worktree.Stage(t.Context(), "dir/kept.txt"))

		unstaged, err := checkout.Worktree.Unstaged(t.Context())
		require.NoError(t, err)
		assert.Equal(t, []string{"tracked.txt"}, unstaged)
	})

	t.Run("Committed reads HEAD's content, not the working tree's", func(t *testing.T) {
		checkout := setup(t)
		writeFile(t, checkout.Path, "tracked.txt", "changed")

		content, err := checkout.Worktree.Committed(t.Context(), "tracked.txt")
		require.NoError(t, err)
		assert.Equal(t, "a", string(content))
	})

	t.Run("Committed fails for a path HEAD does not have", func(t *testing.T) {
		checkout := setup(t)
		writeFile(t, checkout.Path, "untracked.txt", "b")

		_, err := checkout.Worktree.Committed(t.Context(), "untracked.txt")
		assert.ErrorContains(t, err, "git show")
	})

	t.Run("StageGlob matches from the root only", func(t *testing.T) {
		checkout := setup(t)
		writeFile(t, checkout.Path, "composer.json", "{}")
		writeFile(t, checkout.Path, "composer.lock", "{}")
		writeFile(t, checkout.Path, "web/composer.json", "{}")

		require.NoError(t, checkout.Worktree.StageGlob(t.Context(), "composer.*"))

		staged, err := checkout.Worktree.Staged(t.Context())
		require.NoError(t, err)
		slices.Sort(staged)
		assert.Equal(t, []string{"composer.json", "composer.lock"}, staged)
	})
}

func TestRemoveWorktreeRemovesItsDirectoryAndRegistration(t *testing.T) {
	isolateGitConfig(t)
	source := initRepoWithCommit(t)
	service := New()
	checkout, err := service.AddWorktree(t.Context(), source, Identity{})
	require.NoError(t, err)
	writeFile(t, checkout.Path, "dirty.txt", "x")

	require.NoError(t, service.RemoveWorktree(t.Context(), source, checkout.Path))

	assert.NoDirExists(t, filepath.Dir(checkout.Path), "the temporary parent goes too")
	assert.NotContains(t, gitOutput(t, source, "worktree", "list"), checkout.Path)
}

// A directory already gone — removed by hand, or a killed run's — is pruned rather than an error.
func TestRemoveWorktreePrunesAWorktreeWhoseDirectoryIsGone(t *testing.T) {
	isolateGitConfig(t)
	source := initRepoWithCommit(t)
	service := New()
	checkout, err := service.AddWorktree(t.Context(), source, Identity{})
	require.NoError(t, err)
	require.NoError(t, os.RemoveAll(filepath.Dir(checkout.Path)))

	require.NoError(t, service.RemoveWorktree(t.Context(), source, checkout.Path))

	assert.NotContains(t, gitOutput(t, source, "worktree", "list"), checkout.Path)
}

func TestRemoveWorktreeFailsWhenGitCannotRunAtAll(t *testing.T) {
	service := New()
	dir := t.TempDir()

	err := service.RemoveWorktree(t.Context(), filepath.Join(dir, "missing"), filepath.Join(dir, "x", "checkout"))

	require.ErrorContains(t, err, "failed to remove the worktree at")
	require.ErrorIs(t, err, fs.ErrNotExist, "git could not even start in a directory that is not there")
}

func TestAddWorktreeErrors(t *testing.T) {
	isolateGitConfig(t)
	service := New()

	t.Run("not a repository", func(t *testing.T) {
		_, err := service.AddWorktree(t.Context(), t.TempDir(), Identity{})

		require.ErrorIs(t, err, ErrNotARepository)
		require.ErrorContains(t, err, "--working-dir must be the repository root")
	})

	t.Run("git refuses", func(t *testing.T) {
		// A repository without a commit has no HEAD to check out.
		dir := initRepo(t)

		_, err := service.AddWorktree(t.Context(), dir, Identity{})

		require.ErrorContains(t, err, "failed to add a worktree of "+dir)
		var exitErr *exec.ExitError
		require.ErrorAs(t, err, &exitErr)
		matches, globErr := filepath.Glob(filepath.Join(os.TempDir(), "drupdater-worktree-*", "checkout"))
		require.NoError(t, globErr)
		for _, m := range matches {
			assert.NotContains(t, gitOutput(t, dir, "worktree", "list"), m)
		}
	})

	t.Run("git reports success but leaves no worktree", func(t *testing.T) {
		source := initRepoWithCommit(t)
		var worktreeCalls [][]string
		silent := &Git{newCommand: func(ctx context.Context, name string, arg ...string) *exec.Cmd {
			if slices.Contains(arg, "worktree") {
				worktreeCalls = append(worktreeCalls, arg)
				return exec.CommandContext(ctx, "true")
			}
			return exec.CommandContext(ctx, name, arg...)
		}}
		t.Setenv("TMPDIR", t.TempDir())

		_, err := silent.AddWorktree(t.Context(), source, Identity{})

		require.ErrorIs(t, err, ErrNotARepository)
		require.Len(t, worktreeCalls, 2)
		assert.Contains(t, worktreeCalls[1], "remove", "the half-made worktree must be removed again")
		matches, globErr := filepath.Glob(filepath.Join(os.TempDir(), "drupdater-worktree-*"))
		require.NoError(t, globErr)
		assert.Empty(t, matches)
	})

	t.Run("no temporary directory", func(t *testing.T) {
		source := initRepoWithCommit(t)
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

// The container runs as root against a checkout the host user owns. Only root can create that
// situation, so elsewhere TestGitCommandsOverrideTheUsersConfig stands in for it.
func TestAddWorktreeOfARepositoryOwnedByAnotherUser(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("needs root to hand the repository to another user")
	}
	isolateGitConfig(t)
	source := initRepoWithCommit(t)
	root, err := os.OpenRoot(source)
	require.NoError(t, err)
	t.Cleanup(func() { _ = root.Close() })
	require.NoError(t, fs.WalkDir(root.FS(), ".", func(path string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return root.Lchown(path, 65534, 65534)
	}))
	service := New()

	checkout, err := service.AddWorktree(t.Context(), source, Identity{})
	require.NoError(t, err)
	branch, err := service.GetCurrentBranch(t.Context(), source)
	require.NoError(t, err)
	assert.Equal(t, "main", branch)
	require.NoError(t, service.RemoveWorktree(t.Context(), source, checkout.Path))
}
