package drush

import (
	"context"
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// drush runs as a subprocess of composer, so composer's 300s default timeout applies — and
// `site:install` and `updatedb` on a real site exceed it comfortably.
func TestComposerEnvironment(t *testing.T) {
	executor := NewCLI(zap.NewNop(), "/opt/include")

	// captureCmd returns the command that actually ran, so the assertions are about the
	// environment the subprocess was given.
	captureCmd := func(t *testing.T) **exec.Cmd {
		t.Helper()
		t.Setenv("GO_WANT_HELPER_PROCESS", "1")
		t.Setenv("GOCOVERDIR", "/tmp")

		cmd := new(*exec.Cmd)
		executor.newCommand = func(ctx context.Context, name string, arg ...string) *exec.Cmd {
			cs := append([]string{"-test.run=TestHelperProcess", "--", name}, arg...)
			c := exec.CommandContext(ctx, os.Args[0], cs...) //nolint:gosec // test helper process
			*cmd = c
			return c
		}
		return cmd
	}

	assertComposerEnvironment := func(t *testing.T, env []string) {
		t.Helper()
		assert.Contains(t, env, "COMPOSER_PROCESS_TIMEOUT=0", "composer must not cap the drush process it spawns")
		assert.Contains(t, env, "COMPOSER_NO_AUDIT=1")
		assert.Contains(t, env, "SITE_NAME=example_site", "the site must still be passed through")
	}

	t.Run("execDrush", func(t *testing.T) {
		cmd := captureCmd(t)

		_, err := executor.execDrush(t.Context(), "/tmp", "example_site", "status")
		require.NoError(t, err)

		assertComposerEnvironment(t, (*cmd).Env)
	})

	t.Run("Facts", func(t *testing.T) {
		cmd := captureCmd(t)

		// The helper prints no JSON; only the environment it was given matters here.
		_, _ = executor.Facts(t.Context(), "/tmp", "example_site")

		assertComposerEnvironment(t, (*cmd).Env)
	})
}
