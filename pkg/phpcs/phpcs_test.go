package phpcs

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestPhpcsRun(t *testing.T) {
	cli := NewCLI(zap.NewNop())

	t.Run("success", func(t *testing.T) {
		data := `{"files":{},"totals":{"errors":2,"warnings":0,"fixable":1}}`
		execCommand = func(_ context.Context, _ string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--", data}
			cs = append(cs, arg...)
			cmd := exec.Command(os.Args[0], cs...)
			cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1", "GOCOVERDIR=/tmp"}
			return cmd
		}
		defer func() { execCommand = exec.CommandContext }()

		result, err := cli.Run(t.Context(), "/tmp")
		assert.NoError(t, err)
		assert.Equal(t, 2, result.Totals.Errors)
		assert.Equal(t, 1, result.Totals.Fixable)
	})

	t.Run("invalid JSON", func(t *testing.T) {
		execCommand = func(_ context.Context, _ string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--", "not-json"}
			cs = append(cs, arg...)
			cmd := exec.Command(os.Args[0], cs...)
			cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1", "GOCOVERDIR=/tmp"}
			return cmd
		}
		defer func() { execCommand = exec.CommandContext }()

		_, err := cli.Run(t.Context(), "/tmp")
		assert.Error(t, err)
	})

	t.Run("exec error", func(t *testing.T) {
		execCommand = func(_ context.Context, _ string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--"}
			cs = append(cs, arg...)
			cmd := exec.Command(os.Args[0], cs...)
			cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1", "GO_HELPER_PROCESS_ERROR=1", "GOCOVERDIR=/tmp"}
			return cmd
		}
		defer func() { execCommand = exec.CommandContext }()

		_, err := cli.Run(t.Context(), "/tmp")
		assert.Error(t, err)
	})
}

func TestPhpcsRunCBF(t *testing.T) {
	cli := NewCLI(zap.NewNop())

	t.Run("success", func(t *testing.T) {
		execCommand = func(_ context.Context, _ string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--", "ok"}
			cs = append(cs, arg...)
			cmd := exec.Command(os.Args[0], cs...)
			cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1", "GOCOVERDIR=/tmp"}
			return cmd
		}
		defer func() { execCommand = exec.CommandContext }()

		err := cli.RunCBF(t.Context(), "/tmp")
		assert.NoError(t, err)
	})

	t.Run("error", func(t *testing.T) {
		execCommand = func(_ context.Context, _ string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--"}
			cs = append(cs, arg...)
			cmd := exec.Command(os.Args[0], cs...)
			cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1", "GO_HELPER_PROCESS_ERROR=1", "GOCOVERDIR=/tmp"}
			return cmd
		}
		defer func() { execCommand = exec.CommandContext }()

		err := cli.RunCBF(t.Context(), "/tmp")
		assert.Error(t, err)
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
