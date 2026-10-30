package repo

import (
	"os"
	"path/filepath"
	"testing"

	git "github.com/go-git/go-git/v5"
	gitConfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"

	"errors"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/go-git/go-git/v5/storage/memory"
)

// remoteToLocalRepo builds a real local repo to list against, rather than faking go-git's ref
// format, so a mismatch with the actual wire format is caught.
func remoteToLocalRepo(t *testing.T, branches ...string) *git.Remote {
	t.Helper()
	dir := t.TempDir()
	r, err := git.PlainInit(dir, false)
	require.NoError(t, err)
	wt, err := r.Worktree()
	require.NoError(t, err)
	hash, err := wt.Commit("init", &git.CommitOptions{
		AllowEmptyCommits: true,
		Author:            &object.Signature{Name: "t", Email: "t@example.com"},
	})
	require.NoError(t, err)
	for _, b := range branches {
		require.NoError(t, r.Storer.SetReference(plumbing.NewHashReference(plumbing.NewBranchReferenceName(b), hash)))
	}

	return git.NewRemote(memory.NewStorage(), &gitConfig.RemoteConfig{
		Name: "origin",
		URLs: []string{dir},
	})
}

func TestBranchExists(t *testing.T) {
	logger := zap.NewNop()
	service := New(logger)

	t.Run("branch found", func(t *testing.T) {
		remote := remoteToLocalRepo(t, "main", "my-feature")
		repo := NewMockRepository(t)
		repo.EXPECT().Remote("origin").Return(remote, nil)

		found, err := service.BranchExists(repo, "my-feature", "")
		require.NoError(t, err)
		assert.True(t, found)
	})

	t.Run("branch not found", func(t *testing.T) {
		remote := remoteToLocalRepo(t, "main")
		repo := NewMockRepository(t)
		repo.EXPECT().Remote("origin").Return(remote, nil)

		found, err := service.BranchExists(repo, "my-feature", "")
		require.NoError(t, err)
		assert.False(t, found)
	})

	t.Run("branch deleted on remote since the last fetch is not reported as existing", func(t *testing.T) {
		// A stale cached ref, left by a branch the host auto-deleted, must not produce a false
		// positive: BranchExists queries the live remote.
		remote := remoteToLocalRepo(t, "main")
		repo := NewMockRepository(t)
		repo.EXPECT().Remote("origin").Return(remote, nil)

		found, err := service.BranchExists(repo, "my-feature", "")
		require.NoError(t, err)
		assert.False(t, found)
	})

	t.Run("remote lookup error", func(t *testing.T) {
		remoteErr := errors.New("no such remote")

		repo := NewMockRepository(t)
		repo.EXPECT().Remote("origin").Return(nil, remoteErr)

		found, err := service.BranchExists(repo, "my-feature", "")
		require.Error(t, err)
		require.ErrorIs(t, err, remoteErr)
		assert.False(t, found)
	})

	t.Run("remote list error", func(t *testing.T) {
		remote := git.NewRemote(memory.NewStorage(), &gitConfig.RemoteConfig{
			Name: "origin",
			URLs: []string{t.TempDir()}, // empty dir: not a git repo, List() must fail
		})
		repo := NewMockRepository(t)
		repo.EXPECT().Remote("origin").Return(remote, nil)

		found, err := service.BranchExists(repo, "my-feature", "")
		require.Error(t, err)
		assert.False(t, found)
	})
}

func TestGetRemoteURL(t *testing.T) {
	logger := zap.NewNop()
	service := New(logger)

	t.Run("returns origin URL with credentials stripped", func(t *testing.T) {
		dir := t.TempDir()
		r, err := git.PlainInit(dir, false)
		require.NoError(t, err)
		_, err = r.CreateRemote(&gitConfig.RemoteConfig{
			Name: "origin",
			URLs: []string{"https://gitlab-ci-token:secret@example.com/group/repo.git"},
		})
		require.NoError(t, err)

		url, err := service.GetRemoteURL(dir)
		require.NoError(t, err)
		assert.Equal(t, "https://example.com/group/repo.git", url)
	})

	t.Run("errors when there is no origin remote", func(t *testing.T) {
		dir := t.TempDir()
		_, err := git.PlainInit(dir, false)
		require.NoError(t, err)

		_, err = service.GetRemoteURL(dir)
		require.Error(t, err)
	})
}

func TestGetCurrentBranch(t *testing.T) {
	logger := zap.NewNop()
	service := New(logger)

	t.Run("returns the branch HEAD points to", func(t *testing.T) {
		dir := t.TempDir()
		r, err := git.PlainInit(dir, false)
		require.NoError(t, err)

		// Commit once so HEAD resolves to a branch.
		wt, err := r.Worktree()
		require.NoError(t, err)
		_, err = wt.Commit("init", &git.CommitOptions{
			AllowEmptyCommits: true,
			Author:            &object.Signature{Name: "t", Email: "t@example.com"},
		})
		require.NoError(t, err)

		branch, err := service.GetCurrentBranch(dir)
		require.NoError(t, err)
		assert.Equal(t, "master", branch)
	})

	t.Run("returns empty string in detached HEAD", func(t *testing.T) {
		dir := t.TempDir()
		r, err := git.PlainInit(dir, false)
		require.NoError(t, err)

		wt, err := r.Worktree()
		require.NoError(t, err)
		hash, err := wt.Commit("init", &git.CommitOptions{
			AllowEmptyCommits: true,
			Author:            &object.Signature{Name: "t", Email: "t@example.com"},
		})
		require.NoError(t, err)

		// Detach HEAD at the commit.
		require.NoError(t, wt.Checkout(&git.CheckoutOptions{Hash: hash}))

		branch, err := service.GetCurrentBranch(dir)
		require.NoError(t, err)
		assert.Empty(t, branch)
	})

	t.Run("errors on a non-repository path", func(t *testing.T) {
		_, err := service.GetCurrentBranch(t.TempDir())
		require.Error(t, err)
	})
}

// initRepoWithCommit creates a repo containing a single empty commit.
func initRepoWithCommit(t *testing.T) (string, *git.Repository) {
	t.Helper()
	dir := t.TempDir()
	r, err := git.PlainInit(dir, false)
	require.NoError(t, err)
	wt, err := r.Worktree()
	require.NoError(t, err)
	_, err = wt.Commit("init", &git.CommitOptions{
		AllowEmptyCommits: true,
		Author:            &object.Signature{Name: "t", Email: "t@example.com"},
	})
	require.NoError(t, err)
	return dir, r
}

func TestGetRemoteURLFallsBackForSCPStyleURLs(t *testing.T) {
	service := New(zap.NewNop())

	dir := t.TempDir()
	r, err := git.PlainInit(dir, false)
	require.NoError(t, err)
	// url.Parse rejects the SCP form ("first path segment in URL cannot contain colon"), so
	// the raw URL has to be returned unchanged rather than dropped.
	_, err = r.CreateRemote(&gitConfig.RemoteConfig{
		Name: "origin",
		URLs: []string{"git@github.com:drupdater/drupdater.git"},
	})
	require.NoError(t, err)

	url, err := service.GetRemoteURL(dir)
	require.NoError(t, err)
	assert.Equal(t, "git@github.com:drupdater/drupdater.git", url)
}

func TestGetRemoteURLErrorsOnNonRepository(t *testing.T) {
	service := New(zap.NewNop())

	_, err := service.GetRemoteURL(t.TempDir())
	require.Error(t, err)
	// The cause must survive the wrapper, so callers can tell "not a repository" apart from
	// "no origin" rather than matching on message text.
	require.ErrorIs(t, err, git.ErrRepositoryNotExists)
}

func TestGetRemoteURLErrorsWhenOriginMissing(t *testing.T) {
	service := New(zap.NewNop())

	dir := t.TempDir()
	_, err := git.PlainInit(dir, false)
	require.NoError(t, err)

	_, err = service.GetRemoteURL(dir)
	require.Error(t, err)
	require.ErrorIs(t, err, git.ErrRemoteNotFound)
}

func TestIsShallowCloneErrorsOnNonRepository(t *testing.T) {
	service := New(zap.NewNop())

	_, err := service.IsShallowClone(t.TempDir())
	require.Error(t, err)
	require.ErrorIs(t, err, git.ErrRepositoryNotExists)
}

func TestCloneRepository(t *testing.T) {
	service := New(zap.NewNop())

	t.Run("clones a branch and prepares the checkout", func(t *testing.T) {
		source, r := initRepoWithCommit(t)
		head, err := r.Head()
		require.NoError(t, err)

		checkout, err := service.CloneRepository(source, head.Name().Short(), "", Identity{Name: "Bot", Email: "bot@example.com"})
		require.NoError(t, err)
		path := checkout.Path
		assert.NotNil(t, checkout.Repository)
		assert.NotNil(t, checkout.Worktree)
		require.NotEmpty(t, path)
		assert.Equal(t, Identity{Name: "Bot", Email: "bot@example.com"}, checkout.Author)

		// The clone lives under a per-URL directory in the temp dir, which is what lets the
		// workflow's cleanup remove just this run's clone.
		assert.True(t, filepath.IsAbs(path))
		assert.DirExists(t, filepath.Join(path, ".git"))

		cloned, err := git.PlainOpen(path)
		require.NoError(t, err)
		cfg, err := cloned.Config()
		require.NoError(t, err)
		assert.Empty(t, cfg.User.Name, "the identity travels with each commit, not in git config")

		t.Cleanup(func() { _ = os.RemoveAll(path) })
	})

	t.Run("clones shallow and without tags", func(t *testing.T) {
		// Both options have no other visible effect and could be dropped silently. Depth keeps
		// the clone cheap; NoTags keeps release tags out of a throwaway clone.
		source, r := initRepoWithCommit(t)

		// Three commits in total, so the clone is distinguishable from both a full clone and a
		// depth-2 one -- go-git still marks a depth-2 clone of a two-commit source as shallow.
		wt, err := r.Worktree()
		require.NoError(t, err)
		var tip plumbing.Hash
		for _, msg := range []string{"second", "third"} {
			tip, err = wt.Commit(msg, &git.CommitOptions{
				AllowEmptyCommits: true,
				Author:            &object.Signature{Name: "t", Email: "t@example.com"},
			})
			require.NoError(t, err)
		}
		_, err = r.CreateTag("v1.0.0", tip, nil)
		require.NoError(t, err)

		head, err := r.Head()
		require.NoError(t, err)

		checkout, err := service.CloneRepository(source, head.Name().Short(), "", Identity{Name: "Bot", Email: "bot@example.com"})
		require.NoError(t, err)
		path := checkout.Path
		t.Cleanup(func() { _ = os.RemoveAll(path) })

		shallow, err := service.IsShallowClone(path)
		require.NoError(t, err)
		assert.True(t, shallow, "Depth: 1 should truncate the history")

		cloneShallow, err := git.PlainOpen(path)
		require.NoError(t, err)
		shallowCommits, err := cloneShallow.Storer.Shallow()
		require.NoError(t, err)
		clonedHead, err := cloneShallow.Head()
		require.NoError(t, err)
		// Compared against HEAD, not the graft-point count: there is one either way, so only
		// the tip being the graft point pins the depth to exactly 1.
		require.Len(t, shallowCommits, 1)
		assert.Equal(t, clonedHead.Hash(), shallowCommits[0])

		cloned, err := git.PlainOpen(path)
		require.NoError(t, err)

		tags, err := cloned.Tags()
		require.NoError(t, err)
		tagCount := 0
		require.NoError(t, tags.ForEach(func(*plumbing.Reference) error { tagCount++; return nil }))
		assert.Zero(t, tagCount, "Tags: NoTags should keep the source's tags out of the clone")
	})

	t.Run("returns the clone error for an unreachable repository", func(t *testing.T) {
		_, err := service.CloneRepository(t.TempDir(), "main", "", Identity{Name: "Bot", Email: "bot@example.com"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "git clone")
		require.ErrorIs(t, err, transport.ErrRepositoryNotFound)
	})

	t.Run("returns the clone error for a branch that does not exist", func(t *testing.T) {
		source, _ := initRepoWithCommit(t)

		_, err := service.CloneRepository(source, "no-such-branch", "", Identity{Name: "Bot", Email: "bot@example.com"})
		require.Error(t, err)
	})

	t.Run("returns the error when the project directory cannot be created", func(t *testing.T) {
		roService := &Git{logger: zap.NewNop(), fs: afero.NewReadOnlyFs(afero.NewMemMapFs())}

		_, err := roService.CloneRepository("https://example.com/repo.git", "main", "", Identity{Name: "Bot", Email: "bot@example.com"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create project directory")
	})
}

func TestIsShallowClone(t *testing.T) {
	service := New(zap.NewNop())

	t.Run("a full checkout is not shallow", func(t *testing.T) {
		dir, _ := initRepoWithCommit(t)

		shallow, err := service.IsShallowClone(dir)
		require.NoError(t, err)
		assert.False(t, shallow)
	})

	t.Run("a depth-limited clone is shallow", func(t *testing.T) {
		source, r := initRepoWithCommit(t)
		wt, err := r.Worktree()
		require.NoError(t, err)
		// A second commit gives the depth-1 clone below ancestry to actually truncate.
		_, err = wt.Commit("second", &git.CommitOptions{
			AllowEmptyCommits: true,
			Author:            &object.Signature{Name: "t", Email: "t@example.com"},
		})
		require.NoError(t, err)
		head, err := r.Head()
		require.NoError(t, err)

		checkout, err := service.CloneRepository(source, head.Name().Short(), "", Identity{Name: "Bot", Email: "bot@example.com"})
		require.NoError(t, err)
		path := checkout.Path
		t.Cleanup(func() { _ = os.RemoveAll(path) })

		shallow, err := service.IsShallowClone(path)
		require.NoError(t, err)
		assert.True(t, shallow)
	})

	t.Run("errors on a non-repository path", func(t *testing.T) {
		_, err := service.IsShallowClone(t.TempDir())
		require.Error(t, err)
	})
}
