package phpcs

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

// helperStderrMarker is written to stderr by every helper-process run, so tests can tell
// whether the stderr stream was captured separately from the stdout that gets parsed.
const helperStderrMarker = "helper-process-stderr"

// newTestCLI captures the debug output, the only place several steps are observable.
func newTestCLI(t *testing.T) (*CLI, *observer.ObservedLogs) {
	t.Helper()
	core, logs := observer.New(zapcore.DebugLevel)
	return NewCLI(zap.New(core)), logs
}

// stubExec replaces execCommand with the helper-process pattern and records what the
// production code asked for. The returned pointers are populated by the time the call returns.
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

func TestPhpcsRun(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		cli, logs := newTestCLI(t)

		data := `{"files":{"web/modules/custom/a.php":{"errors":2,"warnings":0,"messages":[{"message":"Missing docblock","source":"Drupal.Commenting","severity":5,"fixable":true,"type":"ERROR","line":7,"column":3}]}},"totals":{"errors":2,"warnings":0,"fixable":1}}`
		name, args, cmd := stubExec(t, data, false)

		// A real directory, because the command actually chdir's into it.
		workDir := t.TempDir()

		result, err := cli.Run(t.Context(), workDir)
		require.NoError(t, err)

		// phpcs runs through composer, in the project directory rather than the process's own
		// working directory.
		assert.Equal(t, "composer", *name)
		require.NotNil(t, *cmd)
		assert.Equal(t, workDir, (*cmd).Dir)

		// The runtime-set flags are what stop phpcs exiting non-zero on findings; without them
		// every project with a coding-style issue would look like a tool failure.
		assert.Equal(t, []string{
			"exec", "--", "phpcs", "--report=json", "-q",
			"--runtime-set", "ignore_errors_on_exit", "1",
			"--runtime-set", "ignore_warnings_on_exit", "1",
		}, *args)

		assert.Equal(t, 2, result.Totals.Errors)
		assert.Equal(t, 0, result.Totals.Warnings)
		assert.Equal(t, 1, result.Totals.Fixable)

		// The per-file report is what the code beautifier addon turns into its summary table.
		require.Len(t, result.Files, 1)
		file := result.Files["web/modules/custom/a.php"]
		assert.Equal(t, 2, file.Errors)
		require.Len(t, file.Messages, 1)
		assert.Equal(t, "Missing docblock", file.Messages[0].Message)
		assert.Equal(t, "Drupal.Commenting", file.Messages[0].Source)
		assert.Equal(t, 5, file.Messages[0].Severity)
		assert.True(t, file.Messages[0].Fixable)
		assert.Equal(t, "ERROR", file.Messages[0].Type)
		assert.Equal(t, 7, file.Messages[0].Line)
		assert.Equal(t, 3, file.Messages[0].Column)

		// The debug line carries both streams. That the JSON above parsed at all is what proves
		// stderr stayed out of it -- the reason the two are captured separately.
		debug := logs.FilterLevelExact(zapcore.DebugLevel).All()
		require.Len(t, debug, 1)
		assert.Contains(t, debug[0].Message, `"fixable":1`, "stdout should be logged")
		assert.Contains(t, debug[0].Message, helperStderrMarker, "stderr should be logged")
	})

	t.Run("invalid JSON", func(t *testing.T) {
		cli, _ := newTestCLI(t)
		stubExec(t, "not-json", false)

		_, err := cli.Run(t.Context(), t.TempDir())
		require.Error(t, err)

		var syntaxErr *json.SyntaxError
		require.ErrorAs(t, err, &syntaxErr)
	})

	t.Run("exec error", func(t *testing.T) {
		cli, _ := newTestCLI(t)
		stubExec(t, "", true)

		result, err := cli.Run(t.Context(), t.TempDir())
		require.Error(t, err)

		// The exec failure is returned as-is rather than being mistaken for a parse failure of
		// the empty output.
		var exitErr *exec.ExitError
		require.ErrorAs(t, err, &exitErr)
		assert.Empty(t, result.Files)
		assert.Zero(t, result.Totals.Errors)
	})
}

func TestPhpcsRunCBF(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		cli, logs := newTestCLI(t)
		name, args, cmd := stubExec(t, "ok", false)

		workDir := t.TempDir()
		require.NoError(t, cli.RunCBF(t.Context(), workDir))

		assert.Equal(t, "composer", *name)
		assert.Equal(t, []string{"exec", "--", "phpcbf"}, *args)
		require.NotNil(t, *cmd)
		assert.Equal(t, workDir, (*cmd).Dir)

		// execComposer merges the streams, so the marker and the output share one line.
		debug := logs.FilterLevelExact(zapcore.DebugLevel).All()
		require.Len(t, debug, 1)
		assert.Contains(t, debug[0].Message, "ok")
		assert.Contains(t, debug[0].Message, helperStderrMarker)
	})

	t.Run("error", func(t *testing.T) {
		cli, _ := newTestCLI(t)
		stubExec(t, "", true)

		err := cli.RunCBF(t.Context(), t.TempDir())
		require.Error(t, err)

		var exitErr *exec.ExitError
		require.ErrorAs(t, err, &exitErr)
	})
}

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
