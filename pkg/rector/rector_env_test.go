package rector

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestComposerEnvironment covers what rector shares with every other package that shells out to
// composer: `composer exec -- rector process` runs rector as a subprocess of composer, so
// composer's default 300s process timeout applies to it. A rector pass over a large custom-code
// tree exceeds that, and the run then fails with a killed subprocess rather than a report.
func TestComposerEnvironment(t *testing.T) {
	cli, _ := newTestCLI(t)
	_, _, cmd := stubExec(t, `{"totals":{"changed_files":0},"file_diffs":[]}`, false)

	_, err := cli.Run(t.Context(), "/tmp", []string{"web/modules/custom"})
	require.NoError(t, err)

	// Asserted on the environment the composer subprocess was actually given, rather than on
	// the helper that builds it.
	env := (*cmd).Env
	assert.Contains(t, env, "COMPOSER_PROCESS_TIMEOUT=0", "composer must not cap the process it spawns")
	assert.Contains(t, env, "COMPOSER_NO_AUDIT=1")
	assert.Contains(t, env, "GO_WANT_HELPER_PROCESS=1", "the inherited environment must survive")
}
