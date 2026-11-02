package composer

import (
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/drupdater/drupdater/internal/agent"
)

func TestNewCLI(t *testing.T) {
	logger := zap.NewNop()
	cli := NewCLI(logger)

	require.NotNil(t, cli)
	assert.Equal(t, logger, cli.logger)
	assert.NotNil(t, cli.newCommand)
}

func TestVersionIsWhatTheHandshakeNames(t *testing.T) {
	service, sent := fakeAgentCLI(t, agentFixture(t, "handshake/response.json"), 0)

	versions, err := service.Version(t.Context())

	require.NoError(t, err)
	assert.Equal(t, Versions{Composer: "2.10.1", PHP: "8.3.0"}, versions)
	request, args := sent()
	assert.JSONEq(t, agentFixture(t, "handshake/request.json"), request)
	assert.Equal(t, []string{"composer", "drupdater:handshake", "--no-interaction", "--no-ansi"}, args)
}

func TestVersionFailsWithoutAnAgentToAsk(t *testing.T) {
	service, _ := fakeAgentCLI(t, agentFixture(t, "handshake/protocol-mismatch.json"), 1)

	versions, err := service.Version(t.Context())

	var agentErr *agent.Error
	require.ErrorAs(t, err, &agentErr)
	require.ErrorContains(t, err, "failed to determine the composer version")
	assert.Equal(t, Versions{}, versions)
}

func TestInstallSendsTheFixture(t *testing.T) {
	service, sent := fakeAgentCLI(t, agentFixture(t, "install/response.json"), 0)

	require.NoError(t, service.Install(t.Context(), t.TempDir()))
	request, args := sent()
	assert.JSONEq(t, agentFixture(t, "install/request.json"), request)
	assert.Equal(t, []string{"composer", "drupdater:install", "--no-interaction", "--no-ansi"}, args)
}

func TestInstallReportsTheAgentsFailure(t *testing.T) {
	service, _ := fakeAgentCLI(t, `{"protocol":5,"command":"drupdater:install","ok":false,`+
		`"error":{"kind":"failed","reason":"composer install failed","message":"Source path not found"}}`, 1)

	err := service.Install(t.Context(), t.TempDir())

	var agentErr *agent.Error
	require.ErrorAs(t, err, &agentErr)
	assert.Equal(t, agent.Failed, agentErr.Kind)
	assert.ErrorContains(t, err, "failed to install dependencies: drupdater:install: failed (composer install failed): Source path not found")
}

func TestHelperProcess(*testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	// Lets a test assert on what the subprocess actually received, rather than on the Env slice
	// the caller assembled for it.
	fmt.Fprintln(os.Stdout, os.Getenv(os.Getenv("GO_HELPER_PROCESS_PRINT_ENV")))
	os.Exit(0)
}
