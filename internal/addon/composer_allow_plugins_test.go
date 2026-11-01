package addon

import (
	"encoding/json"
	"testing"

	"github.com/drupdater/drupdater/internal/composer"
	"github.com/drupdater/drupdater/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestNewDefaultAllowPlugins(t *testing.T) {
	logger := zap.NewNop()
	composerRunner := NewMockAllowPluginsComposer(t)

	ap := NewComposerAllowPlugins(logger, composerRunner)

	assert.Equal(t, logger, ap.logger)
	assert.Equal(t, composerRunner, ap.composer)
}

func TestAllowPluginsOpensWithoutChangingThePlan(t *testing.T) {
	c := NewMockAllowPluginsComposer(t)
	opened := composer.PluginsOpened{Original: json.RawMessage(`{"drupal/*":true}`), Opened: true}
	c.EXPECT().OpenAllowPlugins(anyCtx, "/p").Return(opened, nil)

	proposal, err := NewComposerAllowPlugins(zap.NewNop(), c).preComposerUpdateHandler(
		workflow.NewPreComposerUpdateEvent(t.Context(), "/p", nil, workflow.UpdatePlan{}))

	require.NoError(t, err)
	assert.Zero(t, proposal.Plan, "allowing plugins never changes what the update touches")
	assert.Empty(t, proposal.Commits)
	assert.Equal(t, opened, proposal.Finding)
}

func TestAllowPluginsClosesWithWhatItsOpeningRecorded(t *testing.T) {
	c := NewMockAllowPluginsComposer(t)
	opened := composer.PluginsOpened{Original: json.RawMessage(`null`), Opened: true}
	c.EXPECT().CloseAllowPlugins(anyCtx, "/p", opened).Return([]string{"tbachert/spi"}, nil)
	earlier := &workflow.Findings{}
	earlier.Record(workflow.EventPreComposerUpdate, "", opened, nil)

	result, err := NewComposerAllowPlugins(zap.NewNop(), c).postComposerUpdateHandler(
		workflow.NewPostComposerUpdateEvent(t.Context(), "/p", nil), earlier)

	require.NoError(t, err)
	assert.Equal(t, []string{"tbachert/spi"}, result.Finding)
	assert.Empty(t, result.Commits)
}

func TestAllowPluginsClosesWithNothingRecordedAsNeverOpened(t *testing.T) {
	c := NewMockAllowPluginsComposer(t)
	c.EXPECT().CloseAllowPlugins(anyCtx, "/p", composer.PluginsOpened{}).Return(nil, nil)

	_, err := NewComposerAllowPlugins(zap.NewNop(), c).postComposerUpdateHandler(
		workflow.NewPostComposerUpdateEvent(t.Context(), "/p", nil), &workflow.Findings{})

	require.NoError(t, err)
}

func TestAllowPluginsPassesTheAgentsErrorsOn(t *testing.T) {
	c := NewMockAllowPluginsComposer(t)
	c.EXPECT().OpenAllowPlugins(anyCtx, "/p").Return(composer.PluginsOpened{}, assert.AnError)
	c.EXPECT().CloseAllowPlugins(anyCtx, "/p", composer.PluginsOpened{}).Return(nil, assert.AnError)
	ap := NewComposerAllowPlugins(zap.NewNop(), c)

	_, err := ap.preComposerUpdateHandler(workflow.NewPreComposerUpdateEvent(t.Context(), "/p", nil, workflow.UpdatePlan{}))
	require.ErrorIs(t, err, assert.AnError)
	_, err = ap.postComposerUpdateHandler(workflow.NewPostComposerUpdateEvent(t.Context(), "/p", nil), &workflow.Findings{})
	require.ErrorIs(t, err, assert.AnError)
}

func TestAllowPluginsReportsItsDenialsThroughThePipeline(t *testing.T) {
	c := NewMockAllowPluginsComposer(t)
	opened := composer.PluginsOpened{Original: json.RawMessage(`{"drupal/*":true}`), Opened: true}
	c.EXPECT().OpenAllowPlugins(anyCtx, "/p").Return(opened, nil)
	c.EXPECT().CloseAllowPlugins(anyCtx, "/p", opened).Return([]string{"tbachert/spi"}, nil)
	p := workflow.NewPipeline([]workflow.Addon{NewComposerAllowPlugins(zap.NewNop(), c)})

	_, err := workflow.Settle(p, workflow.NewPreComposerUpdateEvent(t.Context(), "/p", nil, workflow.UpdatePlan{}))
	require.NoError(t, err)
	require.NoError(t, workflow.Fire(p, workflow.NewPostComposerUpdateEvent(t.Context(), "/p", nil)))

	assert.Equal(t, PluginAllowlist{Added: []string{"tbachert/spi"}}, reportSection(p, "composer_allow_plugins"))
}
