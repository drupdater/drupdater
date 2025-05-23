package repo

import (
	"testing"

	git "github.com/go-git/go-git/v5"

	"github.com/stretchr/testify/assert"
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
