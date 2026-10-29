package addon

import (
	"context"
	"testing"

	"github.com/drupdater/drupdater/internal/golden"
	"github.com/drupdater/drupdater/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestNewDefaultAllowPlugins(t *testing.T) {
	logger := zap.NewNop()
	composerRunner := NewMockComposer(t)

	ap := NewComposerAllowPlugins(logger, composerRunner)

	assert.NotNil(t, ap)
	assert.Equal(t, logger, ap.logger)
	assert.Equal(t, composerRunner, ap.composer)
}

func TestDefaultAllowPlugins_PreComposerUpdateHandler(t *testing.T) {
	logger := zap.NewNop()
	composerRunner := NewMockComposer(t)

	ctx := context.Background()
	path := "/some/path"
	committer := &fakeCommitter{}

	existingPlugins := map[string]bool{
		"existing/plugin": true,
	}
	composerRunner.EXPECT().GetAllowPlugins(ctx, path).Return(existingPlugins, nil)
	composerRunner.EXPECT().SetConfig(ctx, path, "allow-plugins", "true").Return(nil)

	ap := NewComposerAllowPlugins(logger, composerRunner)

	e := workflow.NewPreComposerUpdateEvent(ctx, path, committer, []string{}, []string{}, false)
	err := ap.preComposerUpdateHandler(e)

	require.NoError(t, err)
	assert.Equal(t, existingPlugins, ap.allowPlugins)
	composerRunner.AssertExpectations(t)
}

func TestDefaultAllowPlugins_PostComposerUpdateHandler(t *testing.T) {
	logger := zap.NewNop()
	composerRunner := NewMockComposer(t)

	ctx := context.Background()
	path := "/some/path"
	committer := &fakeCommitter{}

	// Setup existing plugins
	existingPlugins := map[string]bool{
		"existing/plugin": true,
	}

	allPlugins := map[string]any{
		"existing/plugin": struct{}{},
		"new/plugin":      struct{}{},
	}
	composerRunner.EXPECT().GetInstalledPlugins(ctx, path).Return(allPlugins, nil)

	// Mock the SetAllowPlugins call with updated plugins
	expectedUpdatedPlugins := map[string]bool{
		"existing/plugin": true,
		"new/plugin":      false,
	}
	composerRunner.EXPECT().SetAllowPlugins(ctx, path, expectedUpdatedPlugins).Return(nil)

	ap := NewComposerAllowPlugins(logger, composerRunner)
	ap.allowPlugins = existingPlugins

	e := workflow.NewPostComposerUpdateEvent(ctx, path, committer)
	err := ap.postComposerUpdateHandler(e)

	require.NoError(t, err)
	assert.Equal(t, []string{"new/plugin"}, ap.newAllowPlugins)
	composerRunner.AssertExpectations(t)
}

func TestDefaultAllowPlugins_RenderTemplate(t *testing.T) {
	logger := zap.NewNop()
	composerRunner := NewMockComposer(t)

	ap := NewComposerAllowPlugins(logger, composerRunner)
	ap.newAllowPlugins = []string{"plugin1", "plugin2"}

	result, err := ap.RenderTemplate()

	require.NoError(t, err)
	golden.Assert(t, "testdata/allowplugins.md", result)
}

func TestDefaultAllowPlugins_RenderTemplate_Empty(t *testing.T) {
	logger := zap.NewNop()
	composerRunner := NewMockComposer(t)

	ap := NewComposerAllowPlugins(logger, composerRunner)
	ap.newAllowPlugins = []string{}

	result, err := ap.RenderTemplate()

	require.NoError(t, err)
	assert.Empty(t, result)
}
