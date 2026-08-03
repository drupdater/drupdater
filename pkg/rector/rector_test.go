package rector

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// newTestCLI captures the debug output, the only place several steps are observable.
func newTestCLI(t *testing.T) (*CLI, *observer.ObservedLogs) {
	t.Helper()
	core, logs := observer.New(zapcore.DebugLevel)
	return NewCLI(zap.New(core)), logs
}

// stubExec replaces execCommand with the helper-process pattern and records what the
// production code asked for. The returned pointers are populated by the time Run returns.
func stubExec(t *testing.T, stdout string, wantErr bool) (name *string, args *[]string, cmd **exec.Cmd) {
	t.Helper()
	name, args, cmd = new(string), new([]string), new(*exec.Cmd)

	execCommand = func(ctx context.Context, n string, arg ...string) *exec.Cmd {
		*name = n
		*args = append([]string(nil), arg...)

		cs := []string{"-test.run=TestHelperProcess", "--", stdout}
		cs = append(cs, arg...)
		c := exec.CommandContext(ctx, os.Args[0], cs...) //nolint:gosec // test helper process
		c.Env = []string{"GO_WANT_HELPER_PROCESS=1", "GOCOVERDIR=/tmp"}
		if wantErr {
			c.Env = append(c.Env, "GO_HELPER_PROCESS_ERROR=1")
		}
		*cmd = c
		return c
	}
	t.Cleanup(func() { execCommand = exec.CommandContext })

	return name, args, cmd
}

func TestRectorRun(t *testing.T) {
	t.Run("empty directories skips exec", func(t *testing.T) {
		cli, logs := newTestCLI(t)

		execCommand = func(_ context.Context, _ string, _ ...string) *exec.Cmd {
			t.Fatal("exec should not be called with empty directories")
			return nil
		}
		t.Cleanup(func() { execCommand = exec.CommandContext })

		result, err := cli.Run(t.Context(), "/tmp", []string{})
		require.NoError(t, err)

		assert.Equal(t, 0, result.Totals.ChangedFiles)
		assert.Equal(t, 0, result.Totals.Errors)

		// Empty but not nil: the distinction survives into the report, where nil marshals to
		// null instead of [].
		assert.NotNil(t, result.FileDiffs)
		assert.Empty(t, result.FileDiffs)
		assert.NotNil(t, result.ChangedFiles)
		assert.Empty(t, result.ChangedFiles)

		assert.Equal(t, 1, logs.FilterMessage("no custom code directories found").Len(),
			"the skipped run should say why it did nothing")
	})

	t.Run("success", func(t *testing.T) {
		cli, logs := newTestCLI(t)

		data := `{"totals":{"changed_files":1,"errors":2},"file_diffs":[{"file":"test.php","diff":"@@ ... @@","applied_rectors":["SomeRector"]}],"changed_files":["test.php"]}`
		name, args, cmd := stubExec(t, data, false)

		// A real directory, because the command actually chdir's into it.
		workDir := t.TempDir()

		result, err := cli.Run(t.Context(), workDir, []string{"web/modules/custom", "web/themes/custom"})
		require.NoError(t, err)

		// rector runs through composer, in the project directory rather than the process's
		// own working directory.
		assert.Equal(t, "composer", *name)
		require.NotNil(t, *cmd)
		assert.Equal(t, workDir, (*cmd).Dir)

		// Every custom code directory has to reach rector as a target -- without this the
		// append that builds the target list could be dropped entirely and no test would care.
		assert.Equal(t, []string{
			"exec", "--", "rector", "process",
			"--config=/opt/drupdater/rector.php",
			"--no-progress-bar", "--no-diffs", "--debug", "--output-format=json",
			"web/modules/custom", "web/themes/custom",
		}, *args)

		assert.Equal(t, 1, result.Totals.ChangedFiles)
		assert.Equal(t, 2, result.Totals.Errors)
		require.Len(t, result.FileDiffs, 1)
		assert.Equal(t, "test.php", result.FileDiffs[0].File)
		assert.Equal(t, []string{"SomeRector"}, result.FileDiffs[0].AppliedRectors)
		assert.Equal(t, []string{"test.php"}, result.ChangedFiles)

		// The debug line carries both streams. That the JSON above parsed at all is what
		// proves stderr stayed out of it -- the reason the two are captured separately.
		debug := logs.FilterLevelExact(zapcore.DebugLevel).All()
		require.Len(t, debug, 1)
		assert.Contains(t, debug[0].Message, `"changed_files":1`, "stdout should be logged")
		assert.Contains(t, debug[0].Message, helperStderrMarker, "stderr should be logged")
	})

	t.Run("exec error", func(t *testing.T) {
		cli, _ := newTestCLI(t)
		stubExec(t, "", true)

		_, err := cli.Run(t.Context(), "/tmp", []string{"web/modules/custom"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to run composer command")

		// The cause has to stay reachable through the wrapper, so callers can inspect the
		// exit status rather than string-matching the message.
		var exitErr *exec.ExitError
		require.ErrorAs(t, err, &exitErr)
	})

	t.Run("invalid JSON", func(t *testing.T) {
		cli, _ := newTestCLI(t)
		stubExec(t, "not-json", false)

		_, err := cli.Run(t.Context(), "/tmp", []string{"web/modules/custom"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to unmarshal JSON")

		var syntaxErr *json.SyntaxError
		require.ErrorAs(t, err, &syntaxErr)
	})
}

// helperStderrMarker is written to stderr by every helper-process run, so tests can tell
// whether execComposerJSON captured stderr separately from the stdout it parses.
const helperStderrMarker = "helper-process-stderr"

func TestHelperProcess(*testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	fmt.Fprintln(os.Stderr, helperStderrMarker)
	if os.Getenv("GO_HELPER_PROCESS_ERROR") == "1" {
		os.Exit(1)
	}
	fmt.Fprintf(os.Stdout, "%v\n", os.Args[3])
	os.Exit(0)
}
