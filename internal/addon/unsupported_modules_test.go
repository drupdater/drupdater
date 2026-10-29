package addon

import (
	"context"
	"errors"
	"testing"

	"github.com/drupdater/drupdater/internal/workflow"
	"github.com/drupdater/drupdater/pkg/drush"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestNewUnsupportedModules(t *testing.T) {
	logger := zap.NewNop()
	mockDrush := NewMockDrush(t)

	um := NewUnsupportedModules(logger, mockDrush)

	assert.NotNil(t, um)
	assert.Equal(t, logger, um.logger)
	assert.Equal(t, mockDrush, um.drush)
	assert.NotNil(t, um.modules)
	assert.Empty(t, um.modules)
}

func TestUnsupportedModules_PreSiteUpdateHandler_Success(t *testing.T) {
	logger := zap.NewNop()
	mockDrush := NewMockDrush(t)
	um := NewUnsupportedModules(logger, mockDrush)

	ctx := context.Background()
	testPath := "/test/path"
	testSite := "default"
	mockModules := []drush.UnsupportedModule{
		{Name: "module_a", InstalledVersion: "1.0.0", RecommendedVersion: "None"},
	}

	committer := &fakeCommitter{}
	mockEvent := workflow.NewPreSiteUpdateEvent(ctx, testPath, committer, testSite)

	mockDrush.EXPECT().GetUnsupportedModules(ctx, testPath, testSite).Return(mockModules, nil)

	err := um.preSiteUpdateHandler(mockEvent)

	require.NoError(t, err)
	assert.Equal(t, mockModules[0], um.modules["module_a"])
}

func TestUnsupportedModules_PreSiteUpdateHandler_Dedupe(t *testing.T) {
	logger := zap.NewNop()
	mockDrush := NewMockDrush(t)
	um := NewUnsupportedModules(logger, mockDrush)

	ctx := context.Background()
	committer := &fakeCommitter{}

	shared := drush.UnsupportedModule{Name: "module_a", InstalledVersion: "1.0.0", RecommendedVersion: "None"}

	mockDrush.EXPECT().GetUnsupportedModules(ctx, "/test/path", "site1").Return([]drush.UnsupportedModule{shared}, nil)
	mockDrush.EXPECT().GetUnsupportedModules(ctx, "/test/path", "site2").Return([]drush.UnsupportedModule{shared}, nil)

	require.NoError(t, um.preSiteUpdateHandler(workflow.NewPreSiteUpdateEvent(ctx, "/test/path", committer, "site1")))
	require.NoError(t, um.preSiteUpdateHandler(workflow.NewPreSiteUpdateEvent(ctx, "/test/path", committer, "site2")))

	assert.Len(t, um.modules, 1)
	assert.Equal(t, shared, um.modules["module_a"])
}

func TestUnsupportedModules_PreSiteUpdateHandler_NoModules(t *testing.T) {
	logger := zap.NewNop()
	mockDrush := NewMockDrush(t)
	um := NewUnsupportedModules(logger, mockDrush)

	ctx := context.Background()
	testPath := "/test/path"
	testSite := "default"

	committer := &fakeCommitter{}
	mockEvent := workflow.NewPreSiteUpdateEvent(ctx, testPath, committer, testSite)

	mockDrush.EXPECT().GetUnsupportedModules(ctx, testPath, testSite).Return(nil, nil)

	err := um.preSiteUpdateHandler(mockEvent)

	require.NoError(t, err)
	assert.Empty(t, um.modules)
}

func TestUnsupportedModules_PreSiteUpdateHandler_Error(t *testing.T) {
	logger := zap.NewNop()
	mockDrush := NewMockDrush(t)
	um := NewUnsupportedModules(logger, mockDrush)

	ctx := context.Background()
	testPath := "/test/path"
	testSite := "default"
	expectedError := errors.New("drush error")

	committer := &fakeCommitter{}
	mockEvent := workflow.NewPreSiteUpdateEvent(ctx, testPath, committer, testSite)

	mockDrush.EXPECT().GetUnsupportedModules(ctx, testPath, testSite).Return(nil, expectedError)

	// Execute - errors are swallowed (logged, not returned): this is a best-effort,
	// informational check that must not abort the run.
	err := um.preSiteUpdateHandler(mockEvent)

	require.NoError(t, err)
	assert.Empty(t, um.modules)
}
