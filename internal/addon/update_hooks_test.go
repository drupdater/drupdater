package addon

import (
	"context"
	"testing"

	"github.com/drupdater/drupdater/internal/drush"
	"github.com/drupdater/drupdater/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestNewUpdateHooks(t *testing.T) {
	logger := zap.NewNop()

	updateHooks := NewUpdateHooks(logger)

	assert.NotNil(t, updateHooks)
	assert.Equal(t, logger, updateHooks.logger)
}

func TestUpdateHooksFindsTheSitesPendingHooks(t *testing.T) {
	updateHooks := NewUpdateHooks(zap.NewNop())
	pending := map[string]drush.UpdateHook{
		"module1_update_8001": {Module: "module1", Description: "Update description"},
	}

	evt := workflow.NewPreSiteUpdateEvent(context.Background(), "/test/path", &fakeCommitter{}, "default", drush.Facts{UpdateHooks: pending})

	found, err := updateHooks.preSiteUpdateHandler(evt)
	require.NoError(t, err)
	assert.Equal(t, pending, found)
}

func TestUpdateHooksLogsOnlyASiteWithPendingHooks(t *testing.T) {
	core, logs := observer.New(zapcore.InfoLevel)
	updateHooks := NewUpdateHooks(zap.New(core))

	evt := workflow.NewPreSiteUpdateEvent(context.Background(), "/test/path", &fakeCommitter{}, "default", drush.Facts{UpdateHooks: map[string]drush.UpdateHook{}})
	found, err := updateHooks.preSiteUpdateHandler(evt)
	require.NoError(t, err)
	assert.Empty(t, found)
	assert.Zero(t, logs.Len())

	pending := map[string]drush.UpdateHook{"node_update_10001": {Module: "node"}}
	evt = workflow.NewPreSiteUpdateEvent(context.Background(), "/test/path", &fakeCommitter{}, "second", drush.Facts{UpdateHooks: pending})
	_, err = updateHooks.preSiteUpdateHandler(evt)
	require.NoError(t, err)
	require.Equal(t, 1, logs.Len())
	assert.Equal(t, map[string]any{"site": "second", "count": int64(1)}, logs.All()[0].ContextMap())
}

func TestUpdateHooksReportsEverySiteItFiredFor(t *testing.T) {
	updateHooks := NewUpdateHooks(zap.NewNop())
	p := workflow.NewPipeline([]workflow.Addon{updateHooks})
	pending := map[string]drush.UpdateHook{"node_update_10001": {Module: "node"}}

	for _, site := range []string{"default", "second"} {
		evt := workflow.NewPreSiteUpdateEvent(t.Context(), "/project", &fakeCommitter{}, site, drush.Facts{UpdateHooks: pending})
		require.NoError(t, workflow.Fire(p, evt))
	}

	assert.Equal(t, map[string]map[string]drush.UpdateHook{"default": pending, "second": pending}, reportSection(p, "update_hooks"))
}
