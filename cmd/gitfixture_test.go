package cmd

import (
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"
)

// gitIn runs git in dir as a test fixture would, with an identity of its own.
func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
	)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
}

// gitInit makes dir a repository on main, without a commit.
func gitInit(t *testing.T, dir string) {
	t.Helper()
	gitIn(t, dir, "init", "--quiet", "--initial-branch=main")
}

// gitInitWithCommit makes dir a repository on main with one empty commit, so HEAD resolves.
func gitInitWithCommit(t *testing.T, dir string) {
	t.Helper()
	gitInit(t, dir)
	gitIn(t, dir, "commit", "--quiet", "--allow-empty", "-m", "init")
}
