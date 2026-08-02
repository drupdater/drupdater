package composer

import (
	"context"
	"os"
	"os/exec"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// helperEnv builds the environment of the fake composer subprocess. Anything passed in is seen
// by the helper process on top of the control variables it needs to run at all.
func helperEnv(extra ...string) []string {
	return append([]string{"GO_WANT_HELPER_PROCESS=1", "GOCOVERDIR=/tmp"}, extra...)
}

// fakeComposer replaces execCommand with the helper process for the duration of the test and
// gives it the supplied environment.
func fakeComposer(t *testing.T, env []string) {
	t.Helper()
	execCommand = func(ctx context.Context, name string, arg ...string) *exec.Cmd {
		cs := append([]string{"-test.run=TestHelperProcess", "--", name}, arg...)
		cmd := exec.CommandContext(ctx, os.Args[0], cs...) //nolint:gosec // test helper process
		cmd.Env = env
		return cmd
	}
	t.Cleanup(func() { execCommand = exec.CommandContext })
}

func TestComposerEnv(t *testing.T) {
	t.Run("forces the settings drupdater depends on when they are absent", func(t *testing.T) {
		got := composerEnv([]string{"PATH=/usr/bin"})

		assert.Contains(t, got, "PATH=/usr/bin", "unrelated entries must survive")
		assert.Contains(t, got, "COMPOSER_PROCESS_TIMEOUT=0")
		assert.Contains(t, got, "COMPOSER_NO_AUDIT=1")
	})

	t.Run("overrides an inherited value, leaving exactly one entry per variable", func(t *testing.T) {
		got := composerEnv([]string{
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
		got := composerEnv([]string{
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
		// Including one that is a bare forced variable name: os.Environ() promises nothing about
		// its entries containing "=", and a name on its own assigns nothing and so overrides
		// nothing.
		got := composerEnv([]string{"NOT_AN_ASSIGNMENT", "COMPOSER_PROCESS_TIMEOUT"})

		assert.Contains(t, got, "NOT_AN_ASSIGNMENT")
		assert.Contains(t, got, "COMPOSER_PROCESS_TIMEOUT")
	})

	t.Run("preserves the order of the entries it keeps", func(t *testing.T) {
		got := composerEnv([]string{"A=1", "COMPOSER_NO_AUDIT=0", "B=2", "C=3"})

		assert.Equal(t, []string{"A=1", "B=2", "C=3"}, kept(got),
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

// kept returns the entries of env that composerEnv did not append itself.
func kept(env []string) []string {
	var result []string
	for _, entry := range env {
		if !slices.Contains(requiredComposerEnv, entry) {
			result = append(result, entry)
		}
	}
	return result
}

func TestExecComposerEnvironmentReachesTheSubprocess(t *testing.T) {
	service := &CLI{logger: zap.NewNop()}

	t.Run("execComposer forces the process timeout over an inherited one", func(t *testing.T) {
		// The whole point of the issue: a 300s cap that nobody configured kills a long
		// `composer update` mid-phase. Assert on what the subprocess actually sees, not on the
		// slice drupdater built, so a value that never survives into the child is caught.
		fakeComposer(t, helperEnv(
			"GO_HELPER_PROCESS_PRINT_ENV=COMPOSER_PROCESS_TIMEOUT",
			"COMPOSER_PROCESS_TIMEOUT=300",
		))

		out, err := service.execComposer(t.Context(), "/tmp", "update")
		require.NoError(t, err)
		assert.Equal(t, "0", out)
	})

	t.Run("execComposer disables the implicit audit", func(t *testing.T) {
		fakeComposer(t, helperEnv("GO_HELPER_PROCESS_PRINT_ENV=COMPOSER_NO_AUDIT"))

		out, err := service.execComposer(t.Context(), "/tmp", "update")
		require.NoError(t, err)
		assert.Equal(t, "1", out)
	})

	t.Run("execComposer passes the rest of the environment through", func(t *testing.T) {
		fakeComposer(t, helperEnv(
			"GO_HELPER_PROCESS_PRINT_ENV=COMPOSER_AUTH",
			`COMPOSER_AUTH={"http-basic":{}}`,
		))

		out, err := service.execComposer(t.Context(), "/tmp", "update")
		require.NoError(t, err)
		assert.JSONEq(t, `{"http-basic":{}}`, out, "private registry credentials must still reach composer")
	})

	t.Run("execComposerJSON forces the same settings", func(t *testing.T) {
		fakeComposer(t, helperEnv(
			"GO_HELPER_PROCESS_PRINT_ENV=COMPOSER_PROCESS_TIMEOUT",
			"COMPOSER_PROCESS_TIMEOUT=300",
		))

		out, err := service.execComposerJSON(t.Context(), "/tmp", "audit", "--format=json")
		require.NoError(t, err)
		assert.Equal(t, "0", out)
	})

	t.Run("execComposerJSON passes the rest of the environment through", func(t *testing.T) {
		fakeComposer(t, helperEnv(
			"GO_HELPER_PROCESS_PRINT_ENV=COMPOSER_AUTH",
			`COMPOSER_AUTH={"http-basic":{}}`,
		))

		out, err := service.execComposerJSON(t.Context(), "/tmp", "audit", "--format=json")
		require.NoError(t, err)
		assert.JSONEq(t, `{"http-basic":{}}`, out)
	})

	t.Run("inherits the process environment when the caller set none", func(t *testing.T) {
		// exec.Cmd with a nil Env inherits os.Environ(); building the forced values on top of it
		// must not drop that inheritance, or composer loses PATH and every other variable it
		// needs.
		t.Setenv("GO_WANT_HELPER_PROCESS", "1")
		t.Setenv("GOCOVERDIR", "/tmp")
		t.Setenv("GO_HELPER_PROCESS_PRINT_ENV", "COMPOSER_AUTH")
		t.Setenv("COMPOSER_AUTH", "inherited")

		execCommand = func(ctx context.Context, name string, arg ...string) *exec.Cmd {
			cs := append([]string{"-test.run=TestHelperProcess", "--", name}, arg...)
			return exec.CommandContext(ctx, os.Args[0], cs...) //nolint:gosec // test helper process
		}
		t.Cleanup(func() { execCommand = exec.CommandContext })

		out, err := service.execComposer(t.Context(), "/tmp", "update")
		require.NoError(t, err)
		assert.Equal(t, "inherited", out)
	})
}
