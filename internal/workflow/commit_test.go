package workflow

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drupdater/drupdater/internal/repo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testAuthor = repo.Identity{Name: "Drupdater Bot", Email: "bot@example.com"}

// gitIn runs git in dir as a test fixture would, with an identity of its own.
func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		"GIT_CONFIG_NOSYSTEM=1", "HOME="+t.TempDir(),
	)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	return strings.TrimSpace(string(out))
}

// newCommitRepo returns a worktree of a real repository whose first commit tracks files, as a run
// gets one, and the worktree's directory.
func newCommitRepo(t *testing.T, files map[string]string) (Worktree, string) {
	t.Helper()
	source := t.TempDir()
	gitIn(t, source, "init", "--quiet", "--initial-branch=main")
	for name, content := range files {
		writeFile(t, source, name, content)
	}
	gitIn(t, source, "add", "--all")
	gitIn(t, source, "commit", "--quiet", "--allow-empty", "-m", "init")

	service := repo.New()
	checkout, err := service.AddWorktree(t.Context(), source, testAuthor)
	require.NoError(t, err)
	t.Cleanup(func() { _ = service.RemoveWorktree(context.Background(), source, checkout.Path) })
	return checkout.Worktree, checkout.Path
}

func writeFile(t *testing.T, dir string, name string, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
}

// headFiles is HEAD's message and the files it changed relative to its parent.
func headFiles(t *testing.T, dir string) (string, []string) {
	t.Helper()
	message := gitIn(t, dir, "log", "-1", "--format=%B")
	files := strings.Fields(gitIn(t, dir, "diff-tree", "--no-commit-id", "--name-only", "-r", "HEAD"))
	return message, files
}

func TestCommitterCommitsExactlyThePathsNamed(t *testing.T) {
	worktree, dir := newCommitRepo(t, map[string]string{
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
	committed, err := committer.Commit(t.Context(), "Update patches",
		"composer.json", "composer.lock", "patches/old.patch", "patches/new.patch", "translations")

	require.NoError(t, err)
	assert.True(t, committed)
	message, files := headFiles(t, dir)
	assert.Equal(t, "Update patches", message)
	assert.ElementsMatch(t, []string{"composer.json", "patches/old.patch", "patches/new.patch", "translations/fr.po"}, files,
		"a deletion, a new file and a directory are staged; an unnamed sibling and a missing path are not")
	assert.Equal(t, "Drupdater Bot <bot@example.com> Drupdater Bot <bot@example.com>",
		gitIn(t, dir, "log", "-1", "--format=%an <%ae> %cn <%ce>"), "signed explicitly, so no identity has to be in git config")

	unstaged, err := worktree.Unstaged(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []string{"translations-backup/x"}, unstaged, "an unnamed change stays in the worktree")

	// A second commit through the same committer: the first must have released it.
	writeFile(t, dir, "translations-backup/x", "now named")
	committed, err = committer.Commit(t.Context(), "Back up translations", "translations-backup")
	require.NoError(t, err)
	assert.True(t, committed)
	message, files = headFiles(t, dir)
	assert.Equal(t, "Back up translations", message)
	assert.Equal(t, []string{"translations-backup/x"}, files)
}

func TestCommitterSkipsPathsWithoutChanges(t *testing.T) {
	worktree, dir := newCommitRepo(t, map[string]string{"composer.json": "{}"})
	before := gitIn(t, dir, "rev-parse", "HEAD")

	committed, err := newGitCommitter(worktree, testAuthor).Commit(t.Context(), "Update patches", "composer.json", "composer.lock")

	require.NoError(t, err)
	assert.False(t, committed)
	assert.Equal(t, before, gitIn(t, dir, "rev-parse", "HEAD"), "no empty commit")
}

func TestCommitterRefusesToCarryAnotherStagedChange(t *testing.T) {
	worktree, dir := newCommitRepo(t, map[string]string{"composer.json": "{}", "web/a.php": "a"})
	before := gitIn(t, dir, "rev-parse", "HEAD")
	writeFile(t, dir, "composer.json", `{"changed":true}`)
	gitIn(t, dir, "add", "composer.json")
	writeFile(t, dir, "web/a.php", "fixed")

	committed, err := newGitCommitter(worktree, testAuthor).Commit(t.Context(), "Update coding styles", "web/a.php")

	require.ErrorContains(t, err, `refusing to commit "Update coding styles": composer.json already staged`)
	assert.False(t, committed)
	assert.Equal(t, before, gitIn(t, dir, "rev-parse", "HEAD"))
}

func TestCommitterFailures(t *testing.T) {
	t.Run("staging", func(t *testing.T) {
		worktree := NewMockWorktree(t)
		worktree.EXPECT().Stage(anyCtx, []string{"web/a.php"}).Return(assert.AnError)

		_, err := newGitCommitter(worktree, testAuthor).Commit(t.Context(), "Remove deprecations", "web/a.php")
		require.ErrorIs(t, err, assert.AnError)
		assert.Contains(t, err.Error(), "failed to stage web/a.php")
	})

	t.Run("reading what is staged", func(t *testing.T) {
		worktree := NewMockWorktree(t)
		worktree.EXPECT().Stage(anyCtx, []string{"web/a.php"}).Return(nil)
		worktree.EXPECT().Staged(anyCtx).Return(nil, assert.AnError)

		_, err := newGitCommitter(worktree, testAuthor).Commit(t.Context(), "Remove deprecations", "web/a.php")
		require.ErrorIs(t, err, assert.AnError)
		assert.Contains(t, err.Error(), "failed to read the staged changes")
	})

	t.Run("committing", func(t *testing.T) {
		worktree := NewMockWorktree(t)
		worktree.EXPECT().Stage(anyCtx, []string{"web/a.php"}).Return(nil)
		worktree.EXPECT().Staged(anyCtx).Return([]string{"web/a.php"}, nil)
		worktree.EXPECT().Commit(anyCtx, "Remove deprecations", testAuthor).Return(assert.AnError)

		committed, err := newGitCommitter(worktree, testAuthor).Commit(t.Context(), "Remove deprecations", "web/a.php")
		require.ErrorIs(t, err, assert.AnError)
		assert.False(t, committed)
		assert.Contains(t, err.Error(), `failed to commit "Remove deprecations"`)
	})
}
