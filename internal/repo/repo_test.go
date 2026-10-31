package repo

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cloneOf returns a working copy whose origin is a fresh repository with these branches.
func cloneOf(t *testing.T, branches ...string) (origin string, checkout Checkout) {
	t.Helper()
	isolateGitConfig(t)
	origin = initRepoWithCommit(t)
	for _, b := range branches {
		gitOutput(t, origin, "branch", b)
	}
	checkout, err := New().CloneRepository(t.Context(), origin, "main", "", Identity{Name: "Bot", Email: "bot@example.com"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(checkout.Path) })
	return origin, checkout
}

func TestRemoteBranchExists(t *testing.T) {
	t.Run("branch found", func(t *testing.T) {
		_, checkout := cloneOf(t, "my-feature")

		found, err := checkout.Repository.RemoteBranchExists(t.Context(), "my-feature", "")
		require.NoError(t, err)
		assert.True(t, found)
	})

	t.Run("branch not found", func(t *testing.T) {
		_, checkout := cloneOf(t, "my-feature-two")

		found, err := checkout.Repository.RemoteBranchExists(t.Context(), "my-feature", "")
		require.NoError(t, err)
		assert.False(t, found, "a name that is only a prefix of another branch does not count")
	})

	t.Run("branch deleted on remote since the last fetch is not reported as existing", func(t *testing.T) {
		// A stale cached ref, left by a branch the host auto-deleted, must not produce a false
		// positive: the live remote is queried.
		_, checkout := cloneOf(t)
		gitOutput(t, checkout.Path, "update-ref", "refs/remotes/origin/my-feature", "HEAD")

		found, err := checkout.Repository.RemoteBranchExists(t.Context(), "my-feature", "")
		require.NoError(t, err)
		assert.False(t, found)
	})

	t.Run("no origin remote", func(t *testing.T) {
		isolateGitConfig(t)
		dir := initRepoWithCommit(t)
		checkout := New().checkout(t.Context(), dir, Identity{})

		_, err := checkout.Repository.RemoteBranchExists(t.Context(), "my-feature", "")
		require.ErrorContains(t, err, "failed to get origin remote")
	})

	t.Run("an origin that is not a repository", func(t *testing.T) {
		_, checkout := cloneOf(t)
		gitOutput(t, checkout.Path, "remote", "set-url", "origin", t.TempDir())

		_, err := checkout.Repository.RemoteBranchExists(t.Context(), "my-feature", "")
		require.ErrorContains(t, err, "failed to list remote refs")
	})
}

func TestPushCreatesTheBranchOnOrigin(t *testing.T) {
	origin, checkout := cloneOf(t)
	// A pushed branch cannot update a non-bare origin's checked-out branch, but any other can.
	writeFile(t, checkout.Path, "update.txt", "new")
	require.NoError(t, checkout.Worktree.Stage(t.Context(), "update.txt"))
	require.NoError(t, checkout.Worktree.Commit(t.Context(), "Update", checkout.Author))
	head, err := checkout.Repository.Head(t.Context())
	require.NoError(t, err)
	require.NoError(t, checkout.Repository.SetBranch(t.Context(), "update-abc", head))

	require.NoError(t, checkout.Repository.Push(t.Context(), "update-abc", ""))

	assert.Equal(t, head, gitOutput(t, origin, "rev-parse", "refs/heads/update-abc"))
	found, err := checkout.Repository.RemoteBranchExists(t.Context(), "update-abc", "")
	require.NoError(t, err)
	assert.True(t, found)
}

func TestPushFailsForABranchThatDoesNotExist(t *testing.T) {
	_, checkout := cloneOf(t)

	require.ErrorContains(t, checkout.Repository.Push(t.Context(), "no-such-branch", ""), "git push")
}

// The token reaches git only through the credential helper's environment: never argv, which any
// process on the host can read, and never a config file.
func TestTheTokenIsHandedToGitThroughTheCredentialHelper(t *testing.T) {
	isolateGitConfig(t)
	dir := initRepo(t)
	rs := New()

	cmd := rs.command(t.Context(), dir, "s3cret-token", "credential", "fill")
	cmd.Stdin = strings.NewReader("protocol=https\nhost=example.com\n\n")
	out, err := cmd.Output()
	require.NoError(t, err)

	assert.Contains(t, string(out), "username=du\n")
	assert.Contains(t, string(out), "password=s3cret-token\n")
	assert.NotContains(t, strings.Join(cmd.Args, " "), "s3cret-token")
	assert.Contains(t, cmd.Env, tokenEnv+"=s3cret-token")
}

func TestWithoutATokenGitIsNeverAskedForCredentials(t *testing.T) {
	isolateGitConfig(t)
	dir := initRepo(t)

	cmd := New().command(t.Context(), dir, "", "credential", "fill")
	cmd.Stdin = strings.NewReader("protocol=https\nhost=example.com\n\n")

	require.Error(t, cmd.Run(), "with no helper and prompting off, git has no credential to give")
	assert.NotContains(t, strings.Join(cmd.Args, " "), "credential.helper")
	assert.Contains(t, cmd.Env, "GIT_TERMINAL_PROMPT=0")
}

func TestGitCommandsOverrideTheUsersConfig(t *testing.T) {
	var args []string
	rs := &Git{newCommand: func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
		args = arg
		return exec.CommandContext(ctx, "true")
	}}

	_, err := rs.run(t.Context(), t.TempDir(), "worktree", "prune")

	require.NoError(t, err)
	assert.Equal(t, []string{
		"-c", "safe.directory=*", "-c", "core.hooksPath=/dev/null", "-c", "commit.gpgSign=false",
		"worktree", "prune",
	}, args)
}

func TestGetRemoteURL(t *testing.T) {
	service := New()

	t.Run("returns origin URL with credentials stripped", func(t *testing.T) {
		isolateGitConfig(t)
		dir := initRepo(t)
		gitOutput(t, dir, "remote", "add", "origin", "https://gitlab-ci-token:secret@example.com/group/repo.git")

		url, err := service.GetRemoteURL(t.Context(), dir)
		require.NoError(t, err)
		assert.Equal(t, "https://example.com/group/repo.git", url)
	})

	t.Run("returns the URL as configured, before the user's insteadOf rewriting", func(t *testing.T) {
		home := isolateGitConfig(t)
		require.NoError(t, os.WriteFile(filepath.Join(home, ".gitconfig"),
			[]byte("[url \"git@example.com:\"]\n\tinsteadOf = https://example.com/\n"), 0o600))
		dir := initRepo(t)
		gitOutput(t, dir, "remote", "add", "origin", "https://example.com/group/repo.git")

		url, err := service.GetRemoteURL(t.Context(), dir)
		require.NoError(t, err)
		assert.Equal(t, "https://example.com/group/repo.git", url)
	})

	t.Run("falls back to the raw URL for the SCP form", func(t *testing.T) {
		// url.Parse rejects the SCP form ("first path segment in URL cannot contain colon"), so
		// the raw URL has to be returned unchanged rather than dropped.
		isolateGitConfig(t)
		dir := initRepo(t)
		gitOutput(t, dir, "remote", "add", "origin", "git@github.com:drupdater/drupdater.git")

		url, err := service.GetRemoteURL(t.Context(), dir)
		require.NoError(t, err)
		assert.Equal(t, "git@github.com:drupdater/drupdater.git", url)
	})

	t.Run("errors when there is no origin remote", func(t *testing.T) {
		isolateGitConfig(t)

		_, err := service.GetRemoteURL(t.Context(), initRepo(t))
		require.ErrorContains(t, err, "failed to get origin remote")
	})

	t.Run("errors when origin has an empty URL", func(t *testing.T) {
		isolateGitConfig(t)
		dir := initRepo(t)
		gitOutput(t, dir, "config", "remote.origin.url", "")

		_, err := service.GetRemoteURL(t.Context(), dir)
		require.EqualError(t, err, "origin remote has no URL")
	})
}

// The cause must survive the wrapper, so callers can tell "not a repository" apart from "no
// origin" rather than matching on message text.
func TestNonRepositoryPathsAreRejected(t *testing.T) {
	isolateGitConfig(t)
	service := New()
	sub := filepath.Join(initRepoWithCommit(t), "web")
	require.NoError(t, os.Mkdir(sub, 0o755))

	for name, path := range map[string]string{
		"a plain directory":              t.TempDir(),
		"a missing directory":            filepath.Join(t.TempDir(), "missing"),
		"a subdirectory of a repository": sub,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := service.GetRemoteURL(t.Context(), path)
			require.ErrorIs(t, err, ErrNotARepository)
			require.ErrorContains(t, err, "--working-dir must be the repository root")

			_, err = service.GetCurrentBranch(t.Context(), path)
			require.ErrorIs(t, err, ErrNotARepository)

			_, err = service.IsShallowClone(t.Context(), path)
			require.ErrorIs(t, err, ErrNotARepository)
		})
	}
}

func TestARepositoryReachedThroughASymlinkIsItsRoot(t *testing.T) {
	isolateGitConfig(t)
	dir := initRepoWithCommit(t)
	link := filepath.Join(t.TempDir(), "link")
	require.NoError(t, os.Symlink(dir, link))

	branch, err := New().GetCurrentBranch(t.Context(), link)

	require.NoError(t, err)
	assert.Equal(t, "main", branch)
}

func TestGetCurrentBranch(t *testing.T) {
	service := New()

	t.Run("returns the branch HEAD points to", func(t *testing.T) {
		isolateGitConfig(t)

		branch, err := service.GetCurrentBranch(t.Context(), initRepoWithCommit(t))
		require.NoError(t, err)
		assert.Equal(t, "main", branch)
	})

	t.Run("returns empty string in detached HEAD", func(t *testing.T) {
		isolateGitConfig(t)
		dir := initRepoWithCommit(t)
		gitOutput(t, dir, "checkout", "--quiet", "--detach")

		branch, err := service.GetCurrentBranch(t.Context(), dir)
		require.NoError(t, err)
		assert.Empty(t, branch)
	})
}

func TestCloneRepository(t *testing.T) {
	service := New()

	t.Run("clones a branch and prepares the checkout", func(t *testing.T) {
		isolateGitConfig(t)
		source := initRepoWithCommit(t)

		checkout, err := service.CloneRepository(t.Context(), source, "main", "", Identity{Name: "Bot", Email: "bot@example.com"})
		require.NoError(t, err)
		path := checkout.Path
		t.Cleanup(func() { _ = os.RemoveAll(path) })
		assert.NotNil(t, checkout.Repository)
		assert.NotNil(t, checkout.Worktree)
		assert.Equal(t, Identity{Name: "Bot", Email: "bot@example.com"}, checkout.Author)

		// The clone lives under a per-URL directory in the temp dir, which is what lets the
		// workflow's cleanup remove just this run's clone.
		assert.True(t, filepath.IsAbs(path))
		assert.DirExists(t, filepath.Join(path, ".git"))
		config, err := os.ReadFile(filepath.Join(path, ".git", "config"))
		require.NoError(t, err)
		assert.NotContains(t, string(config), "[user]", "the identity travels with each commit, not in git config")
	})

	t.Run("clones shallow, the one branch, and without tags", func(t *testing.T) {
		// None of these has another visible effect, so each could be dropped silently: depth
		// keeps the clone cheap, the rest keep other branches and release tags out of it.
		isolateGitConfig(t)
		source := initRepoWithCommit(t)
		commitEmpty(t, source, "second")
		commitEmpty(t, source, "third")
		gitOutput(t, source, "tag", "v1.0.0")
		gitOutput(t, source, "branch", "other")

		// Through file://, as a real remote would be: git ignores --depth for a plain local path.
		checkout, err := service.CloneRepository(t.Context(), "file://"+source, "main", "", Identity{})
		require.NoError(t, err)
		t.Cleanup(func() { _ = os.RemoveAll(checkout.Path) })

		shallow, err := service.IsShallowClone(t.Context(), checkout.Path)
		require.NoError(t, err)
		assert.True(t, shallow)
		assert.Equal(t, "1", gitOutput(t, checkout.Path, "rev-list", "--count", "HEAD"))
		assert.Empty(t, gitOutput(t, checkout.Path, "tag"))
		assert.NotContains(t, gitOutput(t, checkout.Path, "branch", "--remotes"), "other")
	})

	t.Run("returns the clone error for an unreachable repository", func(t *testing.T) {
		isolateGitConfig(t)

		_, err := service.CloneRepository(t.Context(), t.TempDir(), "main", "", Identity{})
		require.ErrorContains(t, err, "git clone")
	})

	t.Run("returns the clone error for a branch that does not exist", func(t *testing.T) {
		isolateGitConfig(t)

		_, err := service.CloneRepository(t.Context(), initRepoWithCommit(t), "no-such-branch", "", Identity{})
		require.ErrorContains(t, err, "no-such-branch")
	})

	t.Run("returns the error when the project directory cannot be created", func(t *testing.T) {
		roService := &Git{fs: afero.NewReadOnlyFs(afero.NewMemMapFs())}

		_, err := roService.CloneRepository(t.Context(), "https://example.com/repo.git", "main", "", Identity{})
		require.ErrorContains(t, err, "failed to create project directory")
	})
}

func TestIsShallowClone(t *testing.T) {
	isolateGitConfig(t)

	shallow, err := New().IsShallowClone(t.Context(), initRepoWithCommit(t))

	require.NoError(t, err)
	assert.False(t, shallow, "a full checkout is not shallow")
}

func TestSplitZDropsTheEmptyTail(t *testing.T) {
	assert.Equal(t, []string{"a b", "c"}, splitZ("a b\x00c\x00"))
	assert.Empty(t, splitZ(""))
	assert.True(t, slices.Equal(literal([]string{"a*"}), []string{":(literal)a*"}))
}
