package php

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// shellCommand runs script through sh instead of composer and records what Command asked for:
// two halves of one question, what was requested and what the process saw.
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

		_, _, err := c.Split(t.Context(), "update", "--dry-run")
		require.NoError(t, err)

		assert.Equal(t, []string{"composer", "update", "--dry-run"}, seen)
	})

	t.Run("runs Program instead when one is named", func(t *testing.T) {
		var seen []string
		c := Command{New: shellCommand("true", &seen), Logger: zap.NewNop(), Program: "/opt/app"}
		_, _, err := c.Split(t.Context(), "internal:fire")
		require.NoError(t, err)
		assert.Equal(t, []string{"/opt/app", "internal:fire"}, seen)
	})

	t.Run("runs in Dir", func(t *testing.T) {
		var seen []string
		dir := t.TempDir()
		c := Command{New: shellCommand("pwd -P", &seen), Logger: zap.NewNop(), Dir: dir}

		out, _, err := c.Split(t.Context(), "about")
		require.NoError(t, err)

		resolved, err := filepath.EvalSymlinks(dir)
		require.NoError(t, err)
		assert.Equal(t, resolved, out)
	})

	t.Run("feeds Stdin to the process", func(t *testing.T) {
		var seen []string
		c := Command{New: shellCommand("cat", &seen), Logger: zap.NewNop(), Stdin: strings.NewReader(`{"protocol":2}`)}

		out, _, err := c.Split(t.Context(), "drupdater:handshake")
		require.NoError(t, err)

		assert.Equal(t, `{"protocol":2}`, out)
	})

	t.Run("ExtraEnv beats the environment composer would otherwise be given", func(t *testing.T) {
		// os/exec keeps the last entry for a repeated key, so the append order in build() is
		// what decides the value — and reversing it is invisible to every other test here.
		var seen []string
		c := Command{
			New:      shellCommand(`printf %s "$COMPOSER_PROCESS_TIMEOUT"`, &seen),
			Logger:   zap.NewNop(),
			ExtraEnv: []string{"COMPOSER_PROCESS_TIMEOUT=99"},
		}

		out, _, err := c.Split(t.Context(), "about")
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

		out, _, err := c.Split(t.Context(), "about")
		require.NoError(t, err)

		assert.Equal(t, "1", out)
	})

	t.Run("layers on the inherited environment rather than replacing it", func(t *testing.T) {
		t.Setenv("DRUPDATER_EXEC_TEST", "inherited")
		var seen []string
		c := Command{New: shellCommand(`printf %s "$DRUPDATER_EXEC_TEST"`, &seen), Logger: zap.NewNop()}

		out, _, err := c.Split(t.Context(), "about")
		require.NoError(t, err)

		assert.Equal(t, "inherited", out)
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

func TestComposerEnv(t *testing.T) {
	t.Run("forces the settings drupdater depends on when they are absent", func(t *testing.T) {
		got := Env([]string{"PATH=/usr/bin"})

		assert.Contains(t, got, "PATH=/usr/bin", "unrelated entries must survive")
		assert.Contains(t, got, "COMPOSER_PROCESS_TIMEOUT=0")
		assert.Contains(t, got, "COMPOSER_NO_AUDIT=1")
	})

	t.Run("overrides an inherited value, leaving exactly one entry per variable", func(t *testing.T) {
		got := Env([]string{
			"COMPOSER_PROCESS_TIMEOUT=300",
			"COMPOSER_NO_AUDIT=0",
			"COMPOSER_AUTH={}",
		})

		// Not merely "the forced value is present": an inherited entry left in place would make
		// the result depend on os/exec's de-duplication order rather than on this function.
		assert.Equal(t, 1, countKey(got, "COMPOSER_PROCESS_TIMEOUT"))
		assert.Equal(t, 1, countKey(got, "COMPOSER_NO_AUDIT"))
		assert.Contains(t, got, "COMPOSER_PROCESS_TIMEOUT=0")
		assert.Contains(t, got, "COMPOSER_NO_AUDIT=1")
		assert.NotContains(t, got, "COMPOSER_PROCESS_TIMEOUT=300")
		assert.NotContains(t, got, "COMPOSER_NO_AUDIT=0")
		assert.Contains(t, got, "COMPOSER_AUTH={}", "COMPOSER_AUTH must be passed through untouched")
	})

	t.Run("leaves the image's policy variables to the environment", func(t *testing.T) {
		// These are deployment policy, not correctness: forcing them would override a developer
		// running the binary outside the image with a composer home of their own choosing.
		got := Env([]string{
			"COMPOSER_HOME=/home/dev/.composer",
			"COMPOSER_CACHE_DIR=/home/dev/.cache/composer",
			"COMPOSER_ALLOW_SUPERUSER=0",
			"COMPOSER_FUND=1",
		})

		assert.Contains(t, got, "COMPOSER_HOME=/home/dev/.composer")
		assert.Contains(t, got, "COMPOSER_CACHE_DIR=/home/dev/.cache/composer")
		assert.Contains(t, got, "COMPOSER_ALLOW_SUPERUSER=0")
		assert.Contains(t, got, "COMPOSER_FUND=1")
	})

	t.Run("keeps entries that are not KEY=VALUE", func(t *testing.T) {
		// Including a bare variable name: os.Environ() does not promise every entry has "=",
		// and a name alone assigns — and so overrides — nothing.
		got := Env([]string{"NOT_AN_ASSIGNMENT", "COMPOSER_PROCESS_TIMEOUT"})

		assert.Contains(t, got, "NOT_AN_ASSIGNMENT")
		assert.Contains(t, got, "COMPOSER_PROCESS_TIMEOUT")
	})

	t.Run("preserves the order of the entries it keeps", func(t *testing.T) {
		got := Env([]string{"A=1", "COMPOSER_NO_AUDIT=0", "B=2", "C=3"})

		assert.Equal(t, []string{"A=1", "B=2", "C=3"}, withoutRequired(got),
			"removing a forced entry must not reorder the rest")
	})
}

// countKey returns how many entries of env assign key.
func countKey(env []string, key string) int {
	count := 0
	for _, entry := range env {
		if len(entry) > len(key) && entry[:len(key)+1] == key+"=" {
			count++
		}
	}
	return count
}
