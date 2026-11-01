package addon

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/drupdater/drupdater/internal/agent"
	"github.com/drupdater/drupdater/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type testFinding struct {
	Count int `json:"count"`
}

func replying(reply agent.HookReply) hookRunner {
	return func(context.Context, string, string, any) (agent.HookReply, error) { return reply, nil }
}

func runAgentHook(t *testing.T, run hookRunner) (workflow.Result[testFinding], error) {
	t.Helper()
	hook := agentHook[*workflow.PostComposerUpdateEvent, testFinding](zap.NewNop(), run, "test_addon", postComposerUpdateEvent)
	return hook(workflow.NewPostComposerUpdateEvent(t.Context(), "/p", nil, nil))
}

func TestAgentHookPassesTheCommitsOnInOrder(t *testing.T) {
	result, err := runAgentHook(t, replying(agent.HookReply{Commits: []agent.HookCommit{
		{Message: "First", Paths: []string{"composer.json"}},
		{Message: "Second", Paths: []string{"a", "b"}},
	}}))

	require.NoError(t, err)
	assert.Equal(t, []workflow.Commit{
		{Message: "First", Paths: []string{"composer.json"}},
		{Message: "Second", Paths: []string{"a", "b"}},
	}, result.Commits)
}

func TestAgentHookDecodesTheFinding(t *testing.T) {
	result, err := runAgentHook(t, replying(agent.HookReply{Finding: json.RawMessage(`{"count":3}`)}))

	require.NoError(t, err)
	assert.Equal(t, testFinding{Count: 3}, result.Finding)
	assert.Nil(t, result.Commits)
}

func TestAgentHookTakesANullOrMissingFindingAsTheZeroOne(t *testing.T) {
	for name, finding := range map[string]json.RawMessage{"null": json.RawMessage(`null`), "missing": nil} {
		t.Run(name, func(t *testing.T) {
			result, err := runAgentHook(t, replying(agent.HookReply{Finding: finding}))

			require.NoError(t, err)
			assert.Equal(t, testFinding{}, result.Finding)
		})
	}
}

func TestAgentHookRefusesAFindingFieldTheReportDoesNotDeclare(t *testing.T) {
	_, err := runAgentHook(t, replying(agent.HookReply{Finding: json.RawMessage(`{"count":3,"secret":"x"}`)}))

	require.ErrorContains(t, err, `test_addon: failed to decode the agent's finding: json: unknown field "secret"`)
}

func TestAgentHookSendsThePathAddonAndPayload(t *testing.T) {
	var gotDir, gotAddon string
	var gotEvent any
	run := func(ctx context.Context, dir string, addon string, event any) (agent.HookReply, error) {
		require.NotNil(t, ctx)
		gotDir, gotAddon, gotEvent = dir, addon, event
		return agent.HookReply{}, nil
	}
	hook := agentHook[*workflow.PostComposerUpdateEvent, testFinding](zap.NewNop(), run, "test_addon", postComposerUpdateEvent)

	_, err := hook(workflow.NewPostComposerUpdateEvent(t.Context(), "/p", nil, json.RawMessage(`{"packages":[]}`)))

	require.NoError(t, err)
	assert.Equal(t, "/p", gotDir)
	assert.Equal(t, "test_addon", gotAddon)
	encoded, err := json.Marshal(gotEvent)
	require.NoError(t, err)
	assert.JSONEq(t, `{"name":"post-composer-update","base_lock":{"packages":[]}}`, string(encoded))
}
