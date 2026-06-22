package rector

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestRectorRun(t *testing.T) {
	cli := NewCLI(zap.NewNop())

	t.Run("empty directories skips exec", func(t *testing.T) {
		execCommand = func(_ context.Context, _ string, _ ...string) *exec.Cmd {
			t.Fatal("exec should not be called with empty directories")
			return nil
		}
		defer func() { execCommand = exec.CommandContext }()

		result, err := cli.Run(t.Context(), "/tmp", []string{})
		require.NoError(t, err)
		assert.Equal(t, 0, result.Totals.ChangedFiles)
		assert.Empty(t, result.FileDiffs)
		assert.Empty(t, result.ChangedFiles)
	})

	t.Run("success", func(t *testing.T) {
		data := `{"totals":{"changed_files":1,"errors":0},"file_diffs":[{"file":"test.php","diff":"@@ ... @@","applied_rectors":["SomeRector"]}],"changed_files":["test.php"]}`
		execCommand = func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--", data}
			cs = append(cs, arg...)
			cmd := exec.CommandContext(ctx, os.Args[0], cs...) //nolint:gosec // test helper process
			cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1", "GOCOVERDIR=/tmp"}
			return cmd
		}
		defer func() { execCommand = exec.CommandContext }()

		result, err := cli.Run(t.Context(), "/tmp", []string{"web/modules/custom"})
		require.NoError(t, err)
		assert.Equal(t, 1, result.Totals.ChangedFiles)
		assert.Len(t, result.FileDiffs, 1)
		assert.Equal(t, "test.php", result.FileDiffs[0].File)
	})

	t.Run("exec error", func(t *testing.T) {
		execCommand = func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--"}
			cs = append(cs, arg...)
			cmd := exec.CommandContext(ctx, os.Args[0], cs...) //nolint:gosec // test helper process
			cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1", "GO_HELPER_PROCESS_ERROR=1", "GOCOVERDIR=/tmp"}
			return cmd
		}
		defer func() { execCommand = exec.CommandContext }()

		_, err := cli.Run(t.Context(), "/tmp", []string{"web/modules/custom"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to run composer command")
	})

	t.Run("invalid JSON", func(t *testing.T) {
		execCommand = func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--", "not-json"}
			cs = append(cs, arg...)
			cmd := exec.CommandContext(ctx, os.Args[0], cs...) //nolint:gosec // test helper process
			cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1", "GOCOVERDIR=/tmp"}
			return cmd
		}
		defer func() { execCommand = exec.CommandContext }()

		_, err := cli.Run(t.Context(), "/tmp", []string{"web/modules/custom"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to unmarshal JSON")
	})
}

func TestHelperProcess(*testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	if os.Getenv("GO_HELPER_PROCESS_ERROR") == "1" {
		os.Exit(1)
	}
	fmt.Fprintf(os.Stdout, "%v\n", os.Args[3])
	os.Exit(0)
}
