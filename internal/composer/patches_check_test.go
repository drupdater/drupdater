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

func TestCheckIfPatchesApplySendsTheRequestFixture(t *testing.T) {
	service, sent := fakeAgentCLI(t, agentFixture(t, "patch-check/applies.json"), 0)

	ok, err := service.CheckIfPatchesApply(t.Context(), t.TempDir(), "drupal/token", "1.15.0", []string{
		"/project/patches/token/3123456-local.patch",
		"https://www.drupal.org/files/issues/3200000-8.patch",
	})

	require.NoError(t, err)
	assert.True(t, ok)
	request, args := sent()
	assert.JSONEq(t, agentFixture(t, "patch-check/request.json"), request)
	assert.Equal(t, []string{"composer", "drupdater:patch-check", "--no-interaction", "--no-ansi"}, args)
}

func TestCheckIfPatchesApplySplitsADevVersionsCommit(t *testing.T) {
	service, sent := fakeAgentCLI(t, agentFixture(t, "patch-check/applies.json"), 0)

	_, err := service.CheckIfPatchesApply(t.Context(), t.TempDir(), "drupal/foo", "1.x-dev 0424dff", []string{"/p.patch"})

	require.NoError(t, err)
	request, _ := sent()
	assert.JSONEq(t, `{"protocol":2,"request":{"package":"drupal/foo","version":"1.x-dev","reference":"0424dff","patches":["/p.patch"]}}`, request)
}

func TestCheckIfPatchAppliesChecksOnePatch(t *testing.T) {
	service, sent := fakeAgentCLI(t, agentFixture(t, "patch-check/rejected.json"), 0)

	ok, err := service.CheckIfPatchApplies(t.Context(), t.TempDir(), "drupal/token", "1.15.0", "/project/a.patch")

	require.NoError(t, err)
	assert.False(t, ok, "a rejection is the patch not applying, not a failure to check")
	request, _ := sent()
	assert.JSONEq(t, `{"protocol":2,"request":{"package":"drupal/token","version":"1.15.0","reference":"","patches":["/project/a.patch"]}}`, request)
}

func TestCheckIfPatchesApplyReportsAnUnobtainablePackageAsAnError(t *testing.T) {
	service, _ := fakeAgentCLI(t, agentFixture(t, "patch-check/unobtainable.json"), 1)

	ok, err := service.CheckIfPatchesApply(t.Context(), t.TempDir(), "drupal/token", "1.15.0", []string{"/p.patch"})

	assert.False(t, ok)
	var agentErr *agent.Error
	require.ErrorAs(t, err, &agentErr)
	assert.Equal(t, agent.Unobtainable, agentErr.Kind)
	assert.Equal(t, "repository authentication failed", agentErr.Reason)
	assert.Contains(t, err.Error(), "could not check the patches of drupal/token 1.15.0")
}

func TestCheckIfPatchesApplyRefusesAnUnknownOutcome(t *testing.T) {
	service, _ := fakeAgentCLI(t, `{"protocol":2,"command":"drupdater:patch-check","ok":true,"result":{"outcome":"maybe"}}`, 0)

	ok, err := service.CheckIfPatchesApply(t.Context(), t.TempDir(), "drupal/token", "1.15.0", []string{"/p.patch"})

	assert.False(t, ok)
	require.EqualError(t, err, `drupdater:patch-check answered an unknown outcome "maybe"`)
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
