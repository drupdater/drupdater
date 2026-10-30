package addon

import (
	"context"
	"testing"

	"github.com/drupdater/drupdater/internal/drush"
	"github.com/drupdater/drupdater/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestNewUpdateHooks(t *testing.T) {
	logger := zap.NewNop()

	updateHooks := NewUpdateHooks(logger)

	assert.NotNil(t, updateHooks)
	assert.Equal(t, logger, updateHooks.logger)
	assert.Empty(t, updateHooks.hooks.All())
}

func TestUpdateHooksRecordsTheSitesPendingHooks(t *testing.T) {
	updateHooks := NewUpdateHooks(zap.NewNop())
	pending := map[string]drush.UpdateHook{
		"module1_update_8001": {Module: "module1", Description: "Update description"},
	}

	evt := workflow.NewPreSiteUpdateEvent(context.Background(), "/test/path", &fakeCommitter{}, "default", drush.Facts{UpdateHooks: pending})

	require.NoError(t, updateHooks.preSiteUpdateHandler(evt))
	assert.Equal(t, map[string]map[string]drush.UpdateHook{"default": pending}, updateHooks.hooks.All())
}

func TestUpdateHooksRecordsNothingWithoutPendingHooks(t *testing.T) {
	updateHooks := NewUpdateHooks(zap.NewNop())

	evt := workflow.NewPreSiteUpdateEvent(context.Background(), "/test/path", &fakeCommitter{}, "default", drush.Facts{UpdateHooks: map[string]drush.UpdateHook{}})

	require.NoError(t, updateHooks.preSiteUpdateHandler(evt))
	assert.NotContains(t, updateHooks.hooks.All(), "default")
}
