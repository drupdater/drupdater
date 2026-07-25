package repo

import (
	"os"
	"path/filepath"
	"testing"

	git "github.com/go-git/go-git/v5"
	gitConfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestPathWithin(t *testing.T) {
	tests := []struct {
		name     string
		filePath string
		dir      string
		expected bool
	}{
		{name: "file directly in the directory", filePath: "translations/de.po", dir: "translations", expected: true},
		{name: "file nested deeper in the directory", filePath: "translations/custom/de.po", dir: "translations", expected: true},
		{name: "the directory itself", filePath: "translations", dir: "translations", expected: true},
		{name: "an empty dir matches everything", filePath: "any/file.txt", dir: "", expected: true},
		{name: "unrelated directory", filePath: "modules/custom/x.php", dir: "translations", expected: false},
		// The reason pathWithin exists: a substring test would call these a match.
		{name: "sibling sharing a name prefix", filePath: "translations-backup/de.po", dir: "translations", expected: false},
		{name: "prefix match inside a longer segment", filePath: "web/translationsx/de.po", dir: "web/translations", expected: false},
		{name: "leading and trailing slashes are ignored", filePath: "/translations/de.po", dir: "translations/", expected: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, pathWithin(tt.filePath, tt.dir))
		})
	}
}

func TestIsSomethingStagedInPathIgnoresPrefixSiblings(t *testing.T) {
	service := NewGitRepositoryService(zap.NewNop())

	// A staged file in "translations-backup" must not be reported as a change in
	// "translations": the translations addon commits based on this answer, and a false
	// positive there makes it create a commit with no translation changes in it.
	worktree := NewMockWorktree(t)
	worktree.EXPECT().Status().Return(git.Status{
		"translations-backup/de.po": &git.FileStatus{Staging: git.Modified},
	}, nil)

	assert.False(t, service.IsSomethingStagedInPath(worktree, "translations"))
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

func TestOpenRepositoryRemovesPrepareCommitMsgHook(t *testing.T) {
	service := NewGitRepositoryService(zap.NewNop())

	dir, _ := initRepoWithCommit(t)
	hookPath := filepath.Join(dir, ".git", "hooks", "prepare-commit-msg")
	require.NoError(t, os.MkdirAll(filepath.Dir(hookPath), 0o755))
	require.NoError(t, os.WriteFile(hookPath, []byte("#!/bin/sh\nexit 1\n"), 0o600))

	_, worktree, _, err := service.OpenRepository(dir, "Bot", "bot@example.com")
	require.NoError(t, err)
	assert.NotNil(t, worktree)

	// The hook is removed because it cannot be bypassed with --no-verify.
	assert.NoFileExists(t, hookPath)
}

func TestGetRemoteURLFallsBackForSCPStyleURLs(t *testing.T) {
	service := NewGitRepositoryService(zap.NewNop())

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
	service := NewGitRepositoryService(zap.NewNop())

	_, err := service.GetRemoteURL(t.TempDir())
	require.Error(t, err)
}

func TestCloneRepository(t *testing.T) {
	service := NewGitRepositoryService(zap.NewNop())

	t.Run("clones a branch and prepares the checkout", func(t *testing.T) {
		source, r := initRepoWithCommit(t)
		head, err := r.Head()
		require.NoError(t, err)

		repository, worktree, path, err := service.CloneRepository(source, head.Name().Short(), "", "Bot", "bot@example.com")
		require.NoError(t, err)
		assert.NotNil(t, repository)
		assert.NotNil(t, worktree)
		require.NotEmpty(t, path)

		// The clone lives under a per-URL directory in the temp dir, which is what lets the
		// workflow's cleanup remove just this run's clone.
		assert.True(t, filepath.IsAbs(path))
		assert.DirExists(t, filepath.Join(path, ".git"))

		cloned, err := git.PlainOpen(path)
		require.NoError(t, err)
		cfg, err := cloned.Config()
		require.NoError(t, err)
		assert.Equal(t, "Bot", cfg.User.Name)
		assert.Equal(t, "bot@example.com", cfg.User.Email)

		t.Cleanup(func() { _ = os.RemoveAll(path) })
	})

	t.Run("returns the clone error for an unreachable repository", func(t *testing.T) {
		_, _, _, err := service.CloneRepository(t.TempDir(), "main", "", "Bot", "bot@example.com")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "git clone")
	})

	t.Run("returns the clone error for a branch that does not exist", func(t *testing.T) {
		source, _ := initRepoWithCommit(t)

		_, _, _, err := service.CloneRepository(source, "no-such-branch", "", "Bot", "bot@example.com")
		require.Error(t, err)
	})

	t.Run("returns the error when the project directory cannot be created", func(t *testing.T) {
		roService := &GitRepositoryService{logger: zap.NewNop(), fs: afero.NewReadOnlyFs(afero.NewMemMapFs())}

		_, _, _, err := roService.CloneRepository("https://example.com/repo.git", "main", "", "Bot", "bot@example.com")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create project directory")
	})
}

func TestHead(t *testing.T) {
	// Head() is what StartUpdate uses to capture a checkout-mode run's original state before
	// creating drupdater's own throwaway work branch, so it can restore it if the run fails.
	service := NewGitRepositoryService(zap.NewNop())

	t.Run("resolves to the checked-out branch", func(t *testing.T) {
		dir, _ := initRepoWithCommit(t)

		repository, _, _, err := service.OpenRepository(dir, "Bot", "bot@example.com")
		require.NoError(t, err)

		ref, err := repository.Head()
		require.NoError(t, err)
		assert.True(t, ref.Name().IsBranch())
	})

	t.Run("resolves directly to a commit hash when detached", func(t *testing.T) {
		dir, r := initRepoWithCommit(t)
		head, err := r.Head()
		require.NoError(t, err)
		wt, err := r.Worktree()
		require.NoError(t, err)
		require.NoError(t, wt.Checkout(&git.CheckoutOptions{Hash: head.Hash()}))

		repository, _, _, err := service.OpenRepository(dir, "Bot", "bot@example.com")
		require.NoError(t, err)

		ref, err := repository.Head()
		require.NoError(t, err)
		assert.False(t, ref.Name().IsBranch())
		assert.Equal(t, head.Hash(), ref.Hash())
	})
}

func TestIsShallowClone(t *testing.T) {
	service := NewGitRepositoryService(zap.NewNop())

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

		_, _, path, err := service.CloneRepository(source, head.Name().Short(), "", "Bot", "bot@example.com")
		require.NoError(t, err)
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
