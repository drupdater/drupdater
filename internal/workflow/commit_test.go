package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/drupdater/drupdater/internal/repo"
	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// newCommitRepo returns a real repository whose first commit tracks files, and its directory.
func newCommitRepo(t *testing.T, files map[string]string) (*git.Repository, *git.Worktree, string) {
	t.Helper()
	dir := t.TempDir()
	repository, err := git.PlainInit(dir, false)
	require.NoError(t, err)
	worktree, err := repository.Worktree()
	require.NoError(t, err)
	for name, content := range files {
		writeFile(t, dir, name, content)
		_, err := worktree.Add(name)
		require.NoError(t, err)
	}
	_, err = worktree.Commit("init", &git.CommitOptions{Author: &object.Signature{Name: "t", Email: "t@t"}})
	require.NoError(t, err)
	return repository, worktree, dir
}

func writeFile(t *testing.T, dir string, name string, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
}

// headFiles lists what HEAD's commit changed relative to its parent, with its message.
var testAuthor = repo.Identity{Name: "Drupdater Bot", Email: "bot@example.com"}

func headFiles(t *testing.T, repository *git.Repository) (string, []string) {
	t.Helper()
	ref, err := repository.Head()
	require.NoError(t, err)
	commit, err := repository.CommitObject(ref.Hash())
	require.NoError(t, err)
	parent, err := commit.Parent(0)
	require.NoError(t, err)
	patch, err := parent.Patch(commit)
	require.NoError(t, err)
	var files []string
	for _, fp := range patch.FilePatches() {
		from, to := fp.Files()
		if to != nil {
			files = append(files, to.Path())
		} else {
			files = append(files, from.Path())
		}
	}
	return commit.Message, files
}

func withGitIdentity(t *testing.T) {
	t.Helper()
	// The committer leaves the author to go-git, which reads it from the global git config.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".gitconfig"), []byte("[user]\n\tname = t\n\temail = t@t\n"), 0o600))
}

func TestCommitterCommitsExactlyThePathsNamed(t *testing.T) {
	withGitIdentity(t)
	repository, worktree, dir := newCommitRepo(t, map[string]string{
		"composer.json":         "{}",
		"patches/old.patch":     "old",
		"web/modules/custom/a":  "a",
		"translations/de.po":    "de",
		"translations-backup/x": "x",
	})
	writeFile(t, dir, "composer.json", `{"changed":true}`)
	require.NoError(t, os.Remove(filepath.Join(dir, "patches/old.patch")))
	writeFile(t, dir, "patches/new.patch", "new")
	writeFile(t, dir, "translations/fr.po", "fr")
	writeFile(t, dir, "translations-backup/x", "changed but not named")
	writeFile(t, dir, "scratch.txt", "untracked and not named")

	committer := newGitCommitter(worktree, testAuthor)
	committed, err := committer.Commit("Update patches",
		"composer.json", "composer.lock", "patches/old.patch", "patches/new.patch", "translations")

	require.NoError(t, err)
	assert.True(t, committed)
	message, files := headFiles(t, repository)
	assert.Equal(t, "Update patches", message)
	assert.ElementsMatch(t, []string{"composer.json", "patches/old.patch", "patches/new.patch", "translations/fr.po"}, files,
		"a deletion, a new file and a directory are staged; an unnamed sibling and a missing path are not")
	head, err := repository.Head()
	require.NoError(t, err)
	commit, err := repository.CommitObject(head.Hash())
	require.NoError(t, err)
	author := repo.Identity{Name: commit.Author.Name, Email: commit.Author.Email}
	committedBy := repo.Identity{Name: commit.Committer.Name, Email: commit.Committer.Email}
	assert.Equal(t, testAuthor, author)
	assert.Equal(t, testAuthor, committedBy, "signed explicitly, so no identity has to be in git config")

	status, err := worktree.Status()
	require.NoError(t, err)
	assert.Equal(t, git.Modified, status.File("translations-backup/x").Worktree, "an unnamed change stays in the worktree")

	// A second commit through the same committer: the first must have released it.
	writeFile(t, dir, "translations-backup/x", "now named")
	committed, err = committer.Commit("Back up translations", "translations-backup")
	require.NoError(t, err)
	assert.True(t, committed)
	message, files = headFiles(t, repository)
	assert.Equal(t, "Back up translations", message)
	assert.Equal(t, []string{"translations-backup/x"}, files)
}

func TestCommitterSkipsPathsWithoutChanges(t *testing.T) {
	withGitIdentity(t)
	repository, worktree, _ := newCommitRepo(t, map[string]string{"composer.json": "{}"})
	before, err := repository.Head()
	require.NoError(t, err)

	committed, err := newGitCommitter(worktree, testAuthor).Commit("Update patches", "composer.json", "composer.lock")

	require.NoError(t, err)
	assert.False(t, committed)
	after, err := repository.Head()
	require.NoError(t, err)
	assert.Equal(t, before.Hash(), after.Hash(), "no empty commit")
}

func TestCommitterRefusesToCarryAnotherStagedChange(t *testing.T) {
	withGitIdentity(t)
	repository, worktree, dir := newCommitRepo(t, map[string]string{"composer.json": "{}", "web/a.php": "a"})
	before, err := repository.Head()
	require.NoError(t, err)
	writeFile(t, dir, "composer.json", `{"changed":true}`)
	_, err = worktree.Add("composer.json")
	require.NoError(t, err)
	writeFile(t, dir, "web/a.php", "fixed")

	committed, err := newGitCommitter(worktree, testAuthor).Commit("Update coding styles", "web/a.php")

	require.ErrorContains(t, err, `refusing to commit "Update coding styles": composer.json already staged`)
	assert.False(t, committed)
	after, err := repository.Head()
	require.NoError(t, err)
	assert.Equal(t, before.Hash(), after.Hash())
}

func TestCommitterFailures(t *testing.T) {
	t.Run("staging", func(t *testing.T) {
		worktree := NewMockWorktree(t)
		worktree.EXPECT().Add("web/a.php").Return(plumbing.ZeroHash, assert.AnError)

		_, err := newGitCommitter(worktree, testAuthor).Commit("Remove deprecations", "web/a.php")
		require.ErrorIs(t, err, assert.AnError)
		assert.Contains(t, err.Error(), "failed to stage web/a.php")
	})

	t.Run("reading the status", func(t *testing.T) {
		worktree := NewMockWorktree(t)
		worktree.EXPECT().Add("web/a.php").Return(plumbing.ZeroHash, nil)
		worktree.EXPECT().Status().Return(nil, assert.AnError)

		_, err := newGitCommitter(worktree, testAuthor).Commit("Remove deprecations", "web/a.php")
		require.ErrorIs(t, err, assert.AnError)
		assert.Contains(t, err.Error(), "failed to read worktree status")
	})

	t.Run("committing", func(t *testing.T) {
		worktree := NewMockWorktree(t)
		worktree.EXPECT().Add("web/a.php").Return(plumbing.ZeroHash, nil)
		worktree.EXPECT().Status().Return(git.Status{"web/a.php": {Staging: git.Modified}}, nil)
		worktree.EXPECT().Commit("Remove deprecations", mock.Anything).Return(plumbing.ZeroHash, assert.AnError)

		committed, err := newGitCommitter(worktree, testAuthor).Commit("Remove deprecations", "web/a.php")
		require.ErrorIs(t, err, assert.AnError)
		assert.False(t, committed)
		assert.Contains(t, err.Error(), `failed to commit "Remove deprecations"`)
	})
}
