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
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestNewUnsupportedModules(t *testing.T) {
	// Setup
	logger := zap.NewNop()
	mockDrush := NewMockDrush(t)

	// Execute
	um := NewUnsupportedModules(logger, mockDrush)

	// Assert
	assert.NotNil(t, um)
	assert.Equal(t, logger, um.logger)
	assert.Equal(t, mockDrush, um.drush)
	assert.NotNil(t, um.modules)
	assert.Empty(t, um.modules)
}

func TestUnsupportedModules_SubscribedEvents(t *testing.T) {
	// Setup
	um := &UnsupportedModules{}

	// Execute
	events := um.SubscribedEvents()

	// Assert
	assert.Contains(t, events, "pre-site-update")
	assert.IsType(t, event.ListenerItem{}, events["pre-site-update"])
}

func TestUnsupportedModules_RenderTemplate(t *testing.T) {
	// Setup
	fixture, err := os.ReadFile("testdata/unsupported_modules.md")
	require.NoError(t, err)
	expected := string(fixture)

	logger := zap.NewNop()
	mockDrush := NewMockDrush(t)

	um := NewUnsupportedModules(logger, mockDrush)
	um.modules = map[string]drush.UnsupportedModule{
		"module_b": {Name: "module_b", InstalledVersion: "2.3.1", RecommendedVersion: "3.0.0"},
		"module_a": {Name: "module_a", InstalledVersion: "1.0.0", RecommendedVersion: "None"},
	}

	// Execute
	result, err := um.RenderTemplate()

	// Assert
	require.NoError(t, err)
	assert.Equal(t, expected, result)
}

func TestUnsupportedModules_RenderTemplate_Empty(t *testing.T) {
	// Setup
	logger := zap.NewNop()
	mockDrush := NewMockDrush(t)
	um := NewUnsupportedModules(logger, mockDrush)

	// Execute
	result, err := um.RenderTemplate()

	// Assert
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestUnsupportedModules_PreSiteUpdateHandler_Success(t *testing.T) {
	// Setup
	logger := zap.NewNop()
	mockDrush := NewMockDrush(t)
	um := NewUnsupportedModules(logger, mockDrush)

	ctx := context.Background()
	testPath := "/test/path"
	testSite := "default"
	mockModules := []drush.UnsupportedModule{
		{Name: "module_a", InstalledVersion: "1.0.0", RecommendedVersion: "None"},
	}

	worktree := NewMockWorktree(t)
	mockEvent := services.NewPreSiteUpdateEvent(ctx, testPath, worktree, testSite)

	mockDrush.EXPECT().GetUnsupportedModules(ctx, testPath, testSite).Return(mockModules, nil)

	// Execute
	err := um.preSiteUpdateHandler(mockEvent)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, mockModules[0], um.modules["module_a"])
}

func TestUnsupportedModules_PreSiteUpdateHandler_Dedupe(t *testing.T) {
	// Setup
	logger := zap.NewNop()
	mockDrush := NewMockDrush(t)
	um := NewUnsupportedModules(logger, mockDrush)

	ctx := context.Background()
	worktree := NewMockWorktree(t)

	shared := drush.UnsupportedModule{Name: "module_a", InstalledVersion: "1.0.0", RecommendedVersion: "None"}

	mockDrush.EXPECT().GetUnsupportedModules(ctx, "/test/path", "site1").Return([]drush.UnsupportedModule{shared}, nil)
	mockDrush.EXPECT().GetUnsupportedModules(ctx, "/test/path", "site2").Return([]drush.UnsupportedModule{shared}, nil)

	// Execute
	require.NoError(t, um.preSiteUpdateHandler(services.NewPreSiteUpdateEvent(ctx, "/test/path", worktree, "site1")))
	require.NoError(t, um.preSiteUpdateHandler(services.NewPreSiteUpdateEvent(ctx, "/test/path", worktree, "site2")))

	// Assert
	assert.Len(t, um.modules, 1)
	assert.Equal(t, shared, um.modules["module_a"])
}

func TestUnsupportedModules_PreSiteUpdateHandler_NoModules(t *testing.T) {
	// Setup
	logger := zap.NewNop()
	mockDrush := NewMockDrush(t)
	um := NewUnsupportedModules(logger, mockDrush)

	ctx := context.Background()
	testPath := "/test/path"
	testSite := "default"

	worktree := NewMockWorktree(t)
	mockEvent := services.NewPreSiteUpdateEvent(ctx, testPath, worktree, testSite)

	mockDrush.EXPECT().GetUnsupportedModules(ctx, testPath, testSite).Return(nil, nil)

	// Execute
	err := um.preSiteUpdateHandler(mockEvent)

	// Assert
	require.NoError(t, err)
	assert.Empty(t, um.modules)
}

func TestUnsupportedModules_PreSiteUpdateHandler_Error(t *testing.T) {
	// Setup
	logger := zap.NewNop()
	mockDrush := NewMockDrush(t)
	um := NewUnsupportedModules(logger, mockDrush)

	ctx := context.Background()
	testPath := "/test/path"
	testSite := "default"
	expectedError := errors.New("drush error")

	worktree := NewMockWorktree(t)
	mockEvent := services.NewPreSiteUpdateEvent(ctx, testPath, worktree, testSite)

	mockDrush.EXPECT().GetUnsupportedModules(ctx, testPath, testSite).Return(nil, expectedError)

	// Execute - errors are swallowed (logged, not returned): this is a best-effort,
	// informational check that must not abort the run.
	err := um.preSiteUpdateHandler(mockEvent)

	// Assert
	require.NoError(t, err)
	assert.Empty(t, um.modules)
}
