package composer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/drupdater/drupdater/internal/agent"
)

// agentFixture returns one of agent/protocol's examples on the single line the agent prints.
func agentFixture(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "agent", "protocol", name))
	require.NoError(t, err)
	var compact bytes.Buffer
	require.NoError(t, json.Compact(&compact, raw))
	return compact.String()
}

// fakeAgentCLI is a CLI whose composer answers reply and records the request and arguments.
func fakeAgentCLI(t *testing.T, reply string, status int) (*CLI, func() (string, []string)) {
	stdinFile := filepath.Join(t.TempDir(), "stdin")
	var args []string
	service := &CLI{logger: zap.NewNop(), newCommand: func(ctx context.Context, name string, arg ...string) *exec.Cmd {
		args = append([]string{name}, arg...)
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=TestAgentHelperProcess", "--")
		cmd.Env = []string{
			"GO_WANT_AGENT_HELPER_PROCESS=1",
			"AGENT_STDOUT=" + reply,
			"AGENT_STATUS=" + strconv.Itoa(status),
			"AGENT_STDIN_FILE=" + stdinFile,
			"GOCOVERDIR=/tmp",
		}
		return cmd
	}}
	return service, func() (string, []string) {
		body, err := os.ReadFile(stdinFile)
		require.NoError(t, err)
		return string(body), args
	}
}

func TestAgentHelperProcess(*testing.T) {
	if os.Getenv("GO_WANT_AGENT_HELPER_PROCESS") != "1" {
		return
	}
	body, _ := io.ReadAll(os.Stdin)
	_ = os.WriteFile(os.Getenv("AGENT_STDIN_FILE"), body, 0o600) //nolint:gosec // the test names the file
	fmt.Fprint(os.Stdout, os.Getenv("AGENT_STDOUT"))
	status, _ := strconv.Atoi(os.Getenv("AGENT_STATUS"))
	os.Exit(status)
}

func TestCheckAgentSendsTheHandshakeFixture(t *testing.T) {
	service, sent := fakeAgentCLI(t, agentFixture(t, "handshake/response.json"), 0)

	require.NoError(t, service.CheckAgent(t.Context(), t.TempDir()))
	request, args := sent()
	assert.JSONEq(t, agentFixture(t, "handshake/request.json"), request)
	assert.Equal(t, []string{"composer", "drupdater:handshake", "--no-interaction", "--no-ansi"}, args)
}

func TestCheckAgentReportsAMismatchedProtocol(t *testing.T) {
	service, _ := fakeAgentCLI(t, agentFixture(t, "handshake/protocol-mismatch.json"), 1)

	err := service.CheckAgent(t.Context(), t.TempDir())

	var agentErr *agent.Error
	require.ErrorAs(t, err, &agentErr)
	assert.Equal(t, agent.ProtocolMismatch, agentErr.Kind)
}
