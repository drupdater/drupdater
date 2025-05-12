package addon

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/drupdater/drupdater/internal/services"
	"github.com/drupdater/drupdater/pkg/drush"
	"github.com/gookit/event"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestNewUpdateHooks(t *testing.T) {
	// Setup
	logger := zap.NewNop()
	mockDrush := NewMockDrush(t)

	// Execute
	updateHooks := NewUpdateHooks(logger, mockDrush)

	// Assert
	assert.NotNil(t, updateHooks)
	assert.Equal(t, logger, updateHooks.logger)
	assert.Equal(t, mockDrush, updateHooks.drush)
	assert.NotNil(t, updateHooks.hooks)
	assert.Empty(t, updateHooks.hooks)
}

func TestUpdateHooks_SubscribedEvents(t *testing.T) {
	// Setup
	updateHooks := &UpdateHooks{}

	// Execute
	events := updateHooks.SubscribedEvents()

	// Assert
	assert.Contains(t, events, "pre-site-update")
	assert.IsType(t, event.ListenerItem{}, events["pre-site-update"])
}

func TestUpdateHooks_RenderTemplate(t *testing.T) {
	// Setup
	fixture, err := os.ReadFile("testdata/update_hooks.md")
	assert.NoError(t, err)
	expected := string(fixture)

	logger := zap.NewNop()
	mockDrush := NewMockDrush(t)

	hooks := UpdateHooksPerSite{
		"default": {
			"hook": drush.UpdateHook{
				Description: "description",
			},
		},
	}

	ap := NewUpdateHooks(logger, mockDrush)
	ap.hooks = hooks

	// Execute
	result, err := ap.RenderTemplate()

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, expected, result)
}

func TestUpdateHooks_RenderTemplate_Empty(t *testing.T) {
	// Setup
	logger := zap.NewNop()
	mockDrush := NewMockDrush(t)
	hooks := UpdateHooksPerSite{}

	ap := NewUpdateHooks(logger, mockDrush)
	ap.hooks = hooks

	// Execute
	result, err := ap.RenderTemplate()

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, "", result)
}

func TestUpdateHooks_PreSiteUpdateHandler_Success(t *testing.T) {
	// Setup
	logger := zap.NewNop()
	mockDrush := NewMockDrush(t)
	updateHooks := NewUpdateHooks(logger, mockDrush)

	ctx := context.Background()
	testPath := "/test/path"
	testSite := "default"
	mockHooks := map[string]drush.UpdateHook{
		"module1_update_8001": {
			Module:      "module1",
			Description: "Update description",
		},
	}

	worktree := NewMockWorktree(t)
	mockEvent := services.NewPreSiteUpdateEvent(ctx, testPath, worktree, testSite)

	mockDrush.EXPECT().GetUpdateHooks(ctx, testPath, testSite).Return(mockHooks, nil)

	// Execute
	err := updateHooks.preSiteUpdateHandler(mockEvent)

	// Assert
	assert.NoError(t, err)
	assert.Contains(t, updateHooks.hooks, testSite)
	assert.Equal(t, mockHooks, updateHooks.hooks[testSite])
}

func TestUpdateHooks_PreSiteUpdateHandler_NoHooks(t *testing.T) {
	// Setup
	logger := zap.NewNop()
	mockDrush := NewMockDrush(t)
	updateHooks := NewUpdateHooks(logger, mockDrush)

	ctx := context.Background()
	testPath := "/test/path"
	testSite := "default"
	emptyHooks := map[string]drush.UpdateHook{}

	worktree := NewMockWorktree(t)
	mockEvent := services.NewPreSiteUpdateEvent(ctx, testPath, worktree, testSite)

	mockDrush.EXPECT().GetUpdateHooks(ctx, testPath, testSite).Return(emptyHooks, nil)

	// Execute
	err := updateHooks.preSiteUpdateHandler(mockEvent)

	// Assert
	assert.NoError(t, err)
	assert.NotContains(t, updateHooks.hooks, testSite)
}

func TestUpdateHooks_PreSiteUpdateHandler_Error(t *testing.T) {
	// Setup
	logger := zap.NewNop()
	mockDrush := NewMockDrush(t)
	updateHooks := NewUpdateHooks(logger, mockDrush)

	ctx := context.Background()
	testPath := "/test/path"
	testSite := "default"
	expectedError := errors.New("drush error")

	worktree := NewMockWorktree(t)
	mockEvent := services.NewPreSiteUpdateEvent(ctx, testPath, worktree, testSite)

	mockDrush.EXPECT().GetUpdateHooks(ctx, testPath, testSite).Return(nil, expectedError)

	// Execute
	err := updateHooks.preSiteUpdateHandler(mockEvent)

	// Assert
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get update hooks")
}
