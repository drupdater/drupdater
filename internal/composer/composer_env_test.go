package composer

import (
	"context"
	"os"
	"os/exec"
	"testing"

	"github.com/drupdater/drupdater/internal/php"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// helperEnv builds the environment of the fake composer subprocess. Anything passed in is seen
// by the helper process on top of the control variables it needs to run at all.
func helperEnv(extra ...string) []string {
	return append([]string{"GO_WANT_HELPER_PROCESS=1", "GOCOVERDIR=/tmp"}, extra...)
}

// composerWithEnv is the helper process, given the supplied environment.
func composerWithEnv(env []string) php.CommandFactory {
	return func(ctx context.Context, name string, arg ...string) *exec.Cmd {
		cs := append([]string{"-test.run=TestHelperProcess", "--", name}, arg...)
		cmd := exec.CommandContext(ctx, os.Args[0], cs...) //nolint:gosec // test helper process
		cmd.Env = env
		return cmd
	}
}

func TestExecComposerEnvironmentReachesTheSubprocess(t *testing.T) {
	service := &CLI{logger: zap.NewNop()}

	t.Run("execComposer forces the process timeout over an inherited one", func(t *testing.T) {
		// Asserted on what the subprocess sees, not the slice drupdater built, so a value
		// that never survives into the child is caught.
		service.newCommand = composerWithEnv(helperEnv(
			"GO_HELPER_PROCESS_PRINT_ENV=COMPOSER_PROCESS_TIMEOUT",
			"COMPOSER_PROCESS_TIMEOUT=300",
		))

		out, err := service.execComposer(t.Context(), "/tmp", "update")
		require.NoError(t, err)
		assert.Equal(t, "0", out)
	})

	t.Run("execComposer disables the implicit audit", func(t *testing.T) {
		service.newCommand = composerWithEnv(helperEnv("GO_HELPER_PROCESS_PRINT_ENV=COMPOSER_NO_AUDIT"))

		out, err := service.execComposer(t.Context(), "/tmp", "update")
		require.NoError(t, err)
		assert.Equal(t, "1", out)
	})

	t.Run("execComposer passes the rest of the environment through", func(t *testing.T) {
		service.newCommand = composerWithEnv(helperEnv(
			"GO_HELPER_PROCESS_PRINT_ENV=COMPOSER_AUTH",
			`COMPOSER_AUTH={"http-basic":{}}`,
		))

		out, err := service.execComposer(t.Context(), "/tmp", "update")
		require.NoError(t, err)
		assert.JSONEq(t, `{"http-basic":{}}`, out, "private registry credentials must still reach composer")
	})

	t.Run("execComposerJSON forces the same settings", func(t *testing.T) {
		service.newCommand = composerWithEnv(helperEnv(
			"GO_HELPER_PROCESS_PRINT_ENV=COMPOSER_PROCESS_TIMEOUT",
			"COMPOSER_PROCESS_TIMEOUT=300",
		))

		out, err := service.execComposerJSON(t.Context(), "/tmp", "audit", "--format=json")
		require.NoError(t, err)
		assert.Equal(t, "0", out)
	})

	t.Run("execComposerJSON passes the rest of the environment through", func(t *testing.T) {
		service.newCommand = composerWithEnv(helperEnv(
			"GO_HELPER_PROCESS_PRINT_ENV=COMPOSER_AUTH",
			`COMPOSER_AUTH={"http-basic":{}}`,
		))

		out, err := service.execComposerJSON(t.Context(), "/tmp", "audit", "--format=json")
		require.NoError(t, err)
		assert.JSONEq(t, `{"http-basic":{}}`, out)
	})

	t.Run("inherits the process environment when the caller set none", func(t *testing.T) {
		// A nil Env inherits os.Environ(), which the forced values must not drop — composer
		// would lose PATH.
		t.Setenv("GO_WANT_HELPER_PROCESS", "1")
		t.Setenv("GOCOVERDIR", "/tmp")
		t.Setenv("GO_HELPER_PROCESS_PRINT_ENV", "COMPOSER_AUTH")
		t.Setenv("COMPOSER_AUTH", "inherited")

		service.newCommand = func(ctx context.Context, name string, arg ...string) *exec.Cmd {
			cs := append([]string{"-test.run=TestHelperProcess", "--", name}, arg...)
			return exec.CommandContext(ctx, os.Args[0], cs...) //nolint:gosec // test helper process
		}

		out, err := service.execComposer(t.Context(), "/tmp", "update")
		require.NoError(t, err)
		assert.Equal(t, "inherited", out)
	})
}

func TestWithoutBlocking(t *testing.T) {
	printBlocking := func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "sh", "-c", `printf %s "${COMPOSER_NO_BLOCKING-unset}"`) //nolint:gosec // fixed test script
	}

	t.Run("turns composer's policy blocking off for every invocation", func(t *testing.T) {
		t.Setenv("COMPOSER_NO_BLOCKING", "0")
		service := NewCLI(zap.NewNop(), WithoutBlocking())
		service.newCommand = printBlocking

		out, err := service.execComposer(t.Context(), t.TempDir(), "update")
		require.NoError(t, err)

		assert.Equal(t, "1", out, "it must also win over an inherited value")
	})

	t.Run("leaves blocking to the environment otherwise", func(t *testing.T) {
		t.Setenv("COMPOSER_NO_BLOCKING", "")
		os.Unsetenv("COMPOSER_NO_BLOCKING")
		service := NewCLI(zap.NewNop())
		service.newCommand = printBlocking

		out, err := service.execComposer(t.Context(), t.TempDir(), "update")
		require.NoError(t, err)

		assert.Equal(t, "unset", out)
	})
}
