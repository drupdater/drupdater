package phpcs

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// phpcs runs as a subprocess of composer, so composer's 300s default timeout applies — and a
// phpcbf pass over a large custom-code tree exceeds it.
func TestComposerEnvironment(t *testing.T) {
	t.Run("Run", func(t *testing.T) {
		cli, _ := newTestCLI(t)
		_, _, cmd := stubExec(t, `{"files":{},"totals":{"errors":0,"warnings":0,"fixable":0}}`, false)

		_, err := cli.Run(t.Context(), "/tmp")
		require.NoError(t, err)

		assertComposerEnvironment(t, (*cmd).Env)
	})

	t.Run("RunCBF", func(t *testing.T) {
		cli, _ := newTestCLI(t)
		_, _, cmd := stubExec(t, "", false)

		require.NoError(t, cli.RunCBF(t.Context(), "/tmp"))

		assertComposerEnvironment(t, (*cmd).Env)
	})
}

// assertComposerEnvironment states the requirement on the environment the composer subprocess
// was actually given, rather than on the helper that builds it.
func assertComposerEnvironment(t *testing.T, env []string) {
	t.Helper()
	assert.Contains(t, env, "COMPOSER_PROCESS_TIMEOUT=0", "composer must not cap the process it spawns")
	assert.Contains(t, env, "COMPOSER_NO_AUDIT=1")
	assert.Contains(t, env, "GO_WANT_HELPER_PROCESS=1", "the inherited environment must survive")
}
