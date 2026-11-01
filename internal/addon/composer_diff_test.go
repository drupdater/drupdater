package addon

import (
	"encoding/json"
	"testing"

	"github.com/drupdater/drupdater/internal/agent"
	"github.com/drupdater/drupdater/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

const baseLock = `{"packages":[{"name":"drupal/core","version":"10.3.8"}]}`

func fireComposerDiff(t *testing.T, cd *ComposerDiff) (*workflow.Pipeline, error) {
	t.Helper()
	p := workflow.NewPipeline([]workflow.Addon{cd})
	return p, workflow.Fire(p, workflow.NewPostComposerUpdateEvent(t.Context(), "/test/path", nil, json.RawMessage(baseLock)))
}

func TestComposerDiffSendsTheBaseLockAndReportsTheAgentsChanges(t *testing.T) {
	a := NewMockDiffAgent(t)
	a.EXPECT().RunHook(anyCtx, "/test/path", "composer_diff", postComposerUpdatePayload{
		Name: "post-composer-update", BaseLock: json.RawMessage(baseLock),
	}).Return(agent.HookReply{Finding: json.RawMessage(`[
		{"package":"drupal/core","operation":"upgrade","from":"10.3.8","to":"10.3.9","dev":false,"direct":true,
		 "compare_url":"https://example.com/compare","project_url":"https://example.com/core"},
		{"package":"phpunit/phpunit","operation":"remove","from":"10.5.1","dev":true,"direct":false}
	]`)}, nil)

	p, err := fireComposerDiff(t, NewComposerDiff(zap.NewNop(), a))

	require.NoError(t, err)
	assert.Equal(t, []DependencyChange{
		{Package: "drupal/core", Operation: "upgrade", From: "10.3.8", To: "10.3.9", Direct: true,
			CompareURL: "https://example.com/compare", ProjectURL: "https://example.com/core"},
		{Package: "phpunit/phpunit", Operation: "remove", From: "10.5.1", Dev: true},
	}, reportSection(p, "composer_diff"))
}

func TestComposerDiffLogsWhatTheAgentLogs(t *testing.T) {
	core, logs := observer.New(zap.InfoLevel)
	a := NewMockDiffAgent(t)
	a.EXPECT().RunHook(anyCtx, "/test/path", "composer_diff", postComposerUpdatePayload{
		Name: "post-composer-update", BaseLock: json.RawMessage(baseLock),
	}).Return(agent.HookReply{Log: []string{"dependency diff\nupgrade drupal/core: 10.3.8 → 10.3.9\n"}}, nil)

	_, err := fireComposerDiff(t, NewComposerDiff(zap.New(core), a))

	require.NoError(t, err)
	require.Equal(t, 1, logs.Len())
	assert.Equal(t, "dependency diff\nupgrade drupal/core: 10.3.8 → 10.3.9\n", logs.All()[0].Message)
	assert.Equal(t, "composer_diff", logs.All()[0].ContextMap()["addon"])
}

func TestComposerDiffReportsNothingWhenTheAgentFindsNothing(t *testing.T) {
	a := NewMockDiffAgent(t)
	a.EXPECT().RunHook(anyCtx, "/test/path", "composer_diff", postComposerUpdatePayload{
		Name: "post-composer-update", BaseLock: json.RawMessage(baseLock),
	}).Return(agent.HookReply{Finding: json.RawMessage(`[]`)}, nil)

	p, err := fireComposerDiff(t, NewComposerDiff(zap.NewNop(), a))

	require.NoError(t, err)
	assert.Nil(t, reportSection(p, "composer_diff"))
}

func TestComposerDiffFailsWithTheAgent(t *testing.T) {
	a := NewMockDiffAgent(t)
	a.EXPECT().RunHook(anyCtx, "/test/path", "composer_diff", postComposerUpdatePayload{
		Name: "post-composer-update", BaseLock: json.RawMessage(baseLock),
	}).Return(agent.HookReply{}, assert.AnError)

	_, err := fireComposerDiff(t, NewComposerDiff(zap.NewNop(), a))

	require.ErrorIs(t, err, assert.AnError)
	assert.ErrorContains(t, err, "composer_diff: ")
}
