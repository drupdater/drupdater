package composer

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/drupdater/drupdater/internal/agent"
)

func TestRunHookSendsTheRequestFixtureAndDecodesItsReply(t *testing.T) {
	service, sent := fakeAgentCLI(t, agentFixture(t, "addon/response.json"), 0)
	var request struct {
		Request struct {
			Addon string          `json:"addon"`
			Event json.RawMessage `json:"event"`
		} `json:"request"`
	}
	require.NoError(t, json.Unmarshal([]byte(agentFixture(t, "addon/request.json")), &request))

	reply, err := service.RunHook(t.Context(), t.TempDir(), request.Request.Addon, request.Request.Event)

	require.NoError(t, err)
	body, args := sent()
	assert.JSONEq(t, agentFixture(t, "addon/request.json"), body)
	assert.Equal(t, []string{"composer", "drupdater:addon", "--no-interaction", "--no-ansi"}, args)
	assert.Empty(t, reply.Commits)
	assert.Contains(t, reply.Log[0], "upgrade fixture/a: 1.0.0 → 1.1.0\n")
	var rows []map[string]any
	require.NoError(t, json.Unmarshal(reply.Finding, &rows))
	assert.Equal(t, []any{"fixture/a", "fixture/gone", "fixture/new", "fixture/tool"},
		[]any{rows[0]["package"], rows[1]["package"], rows[2]["package"], rows[3]["package"]})
}

func TestRunHookReportsTheAgentsRefusal(t *testing.T) {
	service, _ := fakeAgentCLI(t, `{"protocol":2,"command":"drupdater:addon","ok":false,`+
		`"error":{"kind":"invalid_request","reason":"no addon \"nope\" runs in the agent","message":""}}`, 1)

	_, err := service.RunHook(t.Context(), t.TempDir(), "nope", struct{}{})

	agentErr, ok := errors.AsType[*agent.Error](err)
	require.True(t, ok, "%v", err)
	assert.Equal(t, agent.InvalidRequest, agentErr.Kind)
}
