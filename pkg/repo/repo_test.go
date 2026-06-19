package repo

import (
	"errors"
	"testing"

	git "github.com/go-git/go-git/v5"
	gitConfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/storer"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// errorReferenceIter is a storer.ReferenceIter that always returns an error from Next.
type errorReferenceIter struct {
	err error
}

func (e *errorReferenceIter) Next() (*plumbing.Reference, error) {
	return nil, e.err
}

func (e *errorReferenceIter) ForEach(func(*plumbing.Reference) error) error {
	return e.err
}

func (e *errorReferenceIter) Close() {}

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

func TestBranchExists(t *testing.T) {
	logger := zap.NewNop()
	service := NewGitRepositoryService(logger)

	const targetBranch = "my-feature"
	const remoteBranchRef = "refs/remotes/origin/my-feature"
	const otherRef = "refs/remotes/origin/main"

	t.Run("branch found", func(t *testing.T) {
		refs := []*plumbing.Reference{
			plumbing.NewReferenceFromStrings(otherRef, "abc123"),
			plumbing.NewReferenceFromStrings(remoteBranchRef, "def456"),
		}
		iter := storer.NewReferenceSliceIter(refs)

		repo := NewMockRepository(t)
		repo.EXPECT().References().Return(iter, nil)

		found, err := service.BranchExists(repo, targetBranch)
		require.NoError(t, err)
		assert.True(t, found)
	})

	t.Run("branch not found", func(t *testing.T) {
		refs := []*plumbing.Reference{
			plumbing.NewReferenceFromStrings(otherRef, "abc123"),
		}
		iter := storer.NewReferenceSliceIter(refs)

		repo := NewMockRepository(t)
		repo.EXPECT().References().Return(iter, nil)

		found, err := service.BranchExists(repo, targetBranch)
		require.NoError(t, err)
		assert.False(t, found)
	})

	t.Run("iterator error", func(t *testing.T) {
		iterErr := errors.New("storage corruption")
		iter := &errorReferenceIter{err: iterErr}

		repo := NewMockRepository(t)
		repo.EXPECT().References().Return(iter, nil)

		found, err := service.BranchExists(repo, targetBranch)
		require.Error(t, err)
		assert.ErrorIs(t, err, iterErr)
		assert.False(t, found)
	})

	t.Run("references error", func(t *testing.T) {
		refsErr := errors.New("network failure")

		repo := NewMockRepository(t)
		repo.EXPECT().References().Return(nil, refsErr)

		found, err := service.BranchExists(repo, targetBranch)
		require.Error(t, err)
		assert.ErrorIs(t, err, refsErr)
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
		assert.Equal(t, "", branch)
	})
}
