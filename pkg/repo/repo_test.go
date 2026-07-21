package repo

import (
	"errors"
	"testing"

	git "github.com/go-git/go-git/v5"
	gitConfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage/memory"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type mockWorktree struct {
	status git.Status
	dir    string
	err    error
}

func TestIsSomethingStaged(t *testing.T) {
	// Setup
	logger := zap.NewNop()
	service := NewGitRepositoryService(logger)

	tests := []struct {
		name     string
		worktree mockWorktree
		expected bool
	}{
		{
			name: "nothing staged",
			worktree: mockWorktree{
				status: git.Status{
					"file1.txt": &git.FileStatus{Staging: git.Unmodified},
				},
				err: nil,
				dir: "",
			},
			expected: false,
		},
		{
			name: "something staged",
			worktree: mockWorktree{
				status: git.Status{
					"file1.txt": &git.FileStatus{Staging: git.Modified},
				},
				err: nil,
				dir: "",
			},
			expected: true,
		},
		{
			name: "something staged in directory",
			worktree: mockWorktree{
				status: git.Status{
					"foo/file1.txt": &git.FileStatus{Staging: git.Modified},
				},
				err: nil,
				dir: "foo",
			},
			expected: true,
		},
		{
			name: "nothing staged in root",
			worktree: mockWorktree{
				status: git.Status{
					"foo/file1.txt": &git.FileStatus{Staging: git.Unmodified},
				},
				err: nil,
				dir: "",
			},
			expected: false,
		},
		{
			name: "nothing staged in specific directory",
			worktree: mockWorktree{
				status: git.Status{
					"bar/file1.txt": &git.FileStatus{Staging: git.Modified},
				},
				err: nil,
				dir: "foo",
			},
			expected: false,
		},
		{
			name: "error getting status",
			worktree: mockWorktree{
				status: git.Status{},
				err:    assert.AnError,
				dir:    "",
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup mock for each test case
			worktree := NewMockWorktree(t)
			worktree.EXPECT().Status().Return(tt.worktree.status, tt.worktree.err)

			// Execute
			result := service.IsSomethingStagedInPath(worktree, tt.worktree.dir)

			// Assert
			assert.Equal(t, tt.expected, result)
		})
	}
}

// remoteToLocalRepo creates a real local git repo with the given branches (all pointing at one
// empty commit) and returns a *git.Remote whose "origin" points at it. BranchExists is exercised
// against this real, live listing rather than a hand-rolled fake of go-git's ref format, so the
// test would catch a mismatch against go-git's actual wire format.
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
	service := NewGitRepositoryService(logger)

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
		// A checkout with a stale cached refs/remotes/origin/my-feature (e.g. left over from a
		// branch that was merged and auto-deleted on the host) must not produce a false
		// positive: BranchExists queries the live remote, not the checkout's own refs.
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
	service := NewGitRepositoryService(logger)

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
	service := NewGitRepositoryService(logger)

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

func TestOpenRepository(t *testing.T) {
	logger := zap.NewNop()
	service := NewGitRepositoryService(logger)

	t.Run("opens the checkout and sets the commit identity", func(t *testing.T) {
		dir := t.TempDir()
		_, err := git.PlainInit(dir, false)
		require.NoError(t, err)

		repository, worktree, path, err := service.OpenRepository(dir, "Bot", "bot@example.com")
		require.NoError(t, err)
		assert.NotNil(t, repository)
		assert.NotNil(t, worktree)
		assert.NotEmpty(t, path)

		// The commit identity must have been written to the repo config.
		r, err := git.PlainOpen(dir)
		require.NoError(t, err)
		cfg, err := r.Config()
		require.NoError(t, err)
		assert.Equal(t, "Bot", cfg.User.Name)
		assert.Equal(t, "bot@example.com", cfg.User.Email)
	})

	t.Run("errors on a non-repository path", func(t *testing.T) {
		_, _, _, err := service.OpenRepository(t.TempDir(), "Bot", "bot@example.com")
		require.Error(t, err)
	})
}
