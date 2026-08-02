package composer

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// Command is shared by every package that drives composer -- drush, phpcs and rector all go
// through `composer exec` -- so its contracts are asserted here rather than inferred from those
// packages' tests, which only ever exercise them by accident.

// shellCommand builds a factory that runs script through sh instead of composer, and records the
// invocation Command asked for. Two things in one because they are two halves of the same
// question: what was requested, and what the process then saw.
func shellCommand(script string, seen *[]string) CommandFactory {
	return func(ctx context.Context, name string, args ...string) *exec.Cmd {
		*seen = append([]string{name}, args...)
		return exec.CommandContext(ctx, "sh", "-c", script) //nolint:gosec // fixed test script
	}
}

func TestCommandBuild(t *testing.T) {
	t.Run("asks the factory for composer with the caller's arguments", func(t *testing.T) {
		var seen []string
		c := Command{New: shellCommand("true", &seen), Logger: zap.NewNop()}

		_, err := c.Combined(t.Context(), "update", "--dry-run")
		require.NoError(t, err)

		assert.Equal(t, []string{"composer", "update", "--dry-run"}, seen)
	})

	t.Run("runs in Dir", func(t *testing.T) {
		var seen []string
		dir := t.TempDir()
		c := Command{New: shellCommand("pwd -P", &seen), Logger: zap.NewNop(), Dir: dir}

		out, err := c.Combined(t.Context(), "about")
		require.NoError(t, err)

		resolved, err := filepath.EvalSymlinks(dir)
		require.NoError(t, err)
		assert.Equal(t, resolved, out)
	})

	t.Run("ExtraEnv beats the environment composer would otherwise be given", func(t *testing.T) {
		// The whole reason ExtraEnv is appended after Env rather than before it. os/exec keeps
		// the last entry for a repeated key, so the order in build() is what decides the value
		// the subprocess reads -- and COMPOSER_PROCESS_TIMEOUT is one Env forces itself.
		// Reversing the append is invisible to every other test in this module.
		var seen []string
		c := Command{
			New:      shellCommand(`printf %s "$COMPOSER_PROCESS_TIMEOUT"`, &seen),
			Logger:   zap.NewNop(),
			ExtraEnv: []string{"COMPOSER_PROCESS_TIMEOUT=99"},
		}

		out, err := c.Combined(t.Context(), "about")
		require.NoError(t, err)

		assert.Equal(t, "99", out)
	})

	t.Run("keeps the forced composer settings when ExtraEnv says nothing about them", func(t *testing.T) {
		var seen []string
		c := Command{
			New:      shellCommand(`printf %s "$COMPOSER_NO_AUDIT"`, &seen),
			Logger:   zap.NewNop(),
			ExtraEnv: []string{"SITE_NAME=second"},
		}

		out, err := c.Combined(t.Context(), "about")
		require.NoError(t, err)

		assert.Equal(t, "1", out)
	})

	t.Run("layers on the inherited environment rather than replacing it", func(t *testing.T) {
		t.Setenv("DRUPDATER_EXEC_TEST", "inherited")
		var seen []string
		c := Command{New: shellCommand(`printf %s "$DRUPDATER_EXEC_TEST"`, &seen), Logger: zap.NewNop()}

		out, err := c.Combined(t.Context(), "about")
		require.NoError(t, err)

		assert.Equal(t, "inherited", out)
	})
}

func TestCommandCombined(t *testing.T) {
	t.Run("merges stdout and stderr", func(t *testing.T) {
		var seen []string
		c := Command{New: shellCommand(`echo out; echo err >&2`, &seen), Logger: zap.NewNop()}

		out, err := c.Combined(t.Context(), "about")
		require.NoError(t, err)

		assert.Contains(t, out, "out")
		assert.Contains(t, out, "err")
	})

	t.Run("strips exactly one trailing newline", func(t *testing.T) {
		// Callers compare this against bare strings and parse it as JSON, so the trailing
		// newline every command emits must go -- and only that one, or output whose last line
		// is deliberately blank comes back altered.
		var seen []string
		c := Command{New: shellCommand(`printf 'a\n\n'`, &seen), Logger: zap.NewNop()}

		out, err := c.Combined(t.Context(), "about")
		require.NoError(t, err)

		assert.Equal(t, "a\n", out)
	})

	t.Run("returns the output alongside the error when the command fails", func(t *testing.T) {
		// Every caller quotes this output in its error message, so a failing command must not
		// come back empty-handed.
		var seen []string
		c := Command{New: shellCommand(`echo "why it failed"; exit 3`, &seen), Logger: zap.NewNop()}

		out, err := c.Combined(t.Context(), "about")

		require.Error(t, err)
		assert.Equal(t, "why it failed", out)
	})
}

func TestCommandSplit(t *testing.T) {
	t.Run("keeps stderr out of stdout", func(t *testing.T) {
		// The reason Split exists: stdout is parsed as JSON by the callers that use it, and a
		// composer or PHP notice on stderr would corrupt the payload.
		var seen []string
		c := Command{New: shellCommand(`echo '{"ok":true}'; echo "PHP Notice" >&2`, &seen), Logger: zap.NewNop()}

		stdout, stderr, err := c.Split(t.Context(), "audit")
		require.NoError(t, err)

		assert.Equal(t, `{"ok":true}`, stdout)
		assert.Equal(t, "PHP Notice", stderr)
	})

	t.Run("returns stderr so a caller can quote it in an error", func(t *testing.T) {
		var seen []string
		c := Command{New: shellCommand(`echo "partial"; echo "the reason" >&2; exit 1`, &seen), Logger: zap.NewNop()}

		stdout, stderr, err := c.Split(t.Context(), "audit")

		require.Error(t, err)
		assert.Equal(t, "partial", stdout)
		assert.Equal(t, "the reason", stderr)
	})

	t.Run("trims the trailing newline off both streams", func(t *testing.T) {
		var seen []string
		c := Command{New: shellCommand(`printf 'o\n'; printf 'e\n' >&2`, &seen), Logger: zap.NewNop()}

		stdout, stderr, err := c.Split(t.Context(), "audit")
		require.NoError(t, err)

		assert.Equal(t, "o", stdout)
		assert.Equal(t, "e", stderr)
	})
}
