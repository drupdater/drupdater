package repo

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestMain runs the tests from a directory outside any repository. A git command that loses its
// directory runs in the process's own, which would otherwise be this project's checkout.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "drupdater-repo-test-")
	if err != nil {
		panic(err)
	}
	if err := os.Chdir(dir); err != nil {
		panic(err)
	}
	// Nor may git look upwards from it for a repository to use.
	os.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(dir))
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

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

// gitOutput runs git in dir as a test fixture would, with an identity of its own.
func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
	)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	return strings.TrimSpace(string(out))
}

func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitOutput(t, dir, "init", "--quiet", "--initial-branch=main")
	return dir
}

// initRepoWithCommit creates a repository on main containing a single empty commit.
func initRepoWithCommit(t *testing.T) string {
	t.Helper()
	dir := initRepo(t)
	commitEmpty(t, dir, "init")
	return dir
}

func commitEmpty(t *testing.T, dir string, message string) {
	t.Helper()
	gitOutput(t, dir, "commit", "--quiet", "--allow-empty", "-m", message)
}

func writeFile(t *testing.T, dir string, name string, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
}
