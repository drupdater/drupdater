package utils

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"

	"github.com/maypok86/otter"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap/zaptest"
)

func TestExecDrush(t *testing.T) {
	logger := zaptest.NewLogger(t)
	cache, _ := otter.MustBuilder[string, string](100).Build()
	executor := NewCommandExecutor(logger, cache).(DefaultCommandExecutor)

	t.Run("successful execution", func(t *testing.T) {
		execCommand = func(_ context.Context, name string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--", name}
			cs = append(cs, arg...)
			cmd := exec.Command(os.Args[0], cs...)
			cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1", "GOCOVERDIR=/tmp"}
			return cmd
		}
		defer func() { execCommand = exec.CommandContext }()

		output, err := executor.ExecDrush(t.Context(), "/tmp", "test_site", "status")
		assert.NoError(t, err)
		assert.Equal(t, "[composer exec -- drush status]", output)
	})

	t.Run("execution failure", func(t *testing.T) {
		execCommand = func(_ context.Context, name string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--", name}
			cs = append(cs, arg...)
			cmd := exec.Command(os.Args[0], cs...)
			cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1", "GO_HELPER_PROCESS_ERROR=1", "GOCOVERDIR=/tmp"}
			return cmd
		}
		defer func() { execCommand = exec.CommandContext }()

		output, err := executor.ExecDrush(t.Context(), "/tmp", "test_site", "status")
		assert.Error(t, err)
		assert.Equal(t, "", output)
	})
}

func TestExecComposer(t *testing.T) {
	logger := zaptest.NewLogger(t)
	cache, _ := otter.MustBuilder[string, string](100).Build()
	executor := NewCommandExecutor(logger, cache).(DefaultCommandExecutor)

	t.Run("successful execution", func(t *testing.T) {
		execCommand = func(_ context.Context, name string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--", name}
			cs = append(cs, arg...)
			cmd := exec.Command(os.Args[0], cs...)
			cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1", "GOCOVERDIR=/tmp"}
			return cmd
		}
		defer func() { execCommand = exec.CommandContext }()

		output, err := executor.ExecComposer(t.Context(), "/tmp", "update")
		assert.NoError(t, err)
		assert.Equal(t, "[composer update]", output)
	})

	t.Run("execution failure", func(t *testing.T) {
		execCommand = func(_ context.Context, name string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--", name}
			cs = append(cs, arg...)
			cmd := exec.Command(os.Args[0], cs...)
			cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1", "GO_HELPER_PROCESS_ERROR=1", "GOCOVERDIR=/tmp"}
			return cmd
		}
		defer func() { execCommand = exec.CommandContext }()

		output, err := executor.ExecComposer(t.Context(), "/tmp", "update")
		assert.Error(t, err)
		assert.Equal(t, "", output)
	})

}

func TestHelperProcess(*testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	if os.Getenv("GO_HELPER_PROCESS_ERROR") == "1" {
		os.Exit(1)
	}
	fmt.Fprintf(os.Stdout, "%v\n", os.Args[3:])
	os.Exit(0)
}
