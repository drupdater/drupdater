package addon

import (
	"context"
	"os"
	"testing"

	"github.com/drupdater/drupdater/internal/services"
	"github.com/go-git/go-git/v5"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestNewDefaultAllowPlugins(t *testing.T) {
	// Setup
	logger := zap.NewNop()
	composerRunner := NewMockComposer(t)

	// Execute
	ap := NewComposerAllowPlugins(logger, composerRunner)

	// Verify
	assert.NotNil(t, ap)
	assert.Equal(t, logger, ap.logger)
	assert.Equal(t, composerRunner, ap.composer)
}

func TestDefaultAllowPlugins_SubscribedEvents(t *testing.T) {
	// Setup
	logger := zap.NewNop()
	composerRunner := NewMockComposer(t)

	// Execute
	ap := NewComposerAllowPlugins(logger, composerRunner)
	events := ap.SubscribedEvents()

	// Verify
	assert.Len(t, events, 2)
	assert.Contains(t, events, "pre-composer-update")
	assert.Contains(t, events, "post-composer-update")
}

func TestDefaultAllowPlugins_PreComposerUpdateHandler(t *testing.T) {
	// Setup
	logger := zap.NewNop()
	composerRunner := NewMockComposer(t)

	ctx := context.Background()
	path := "/some/path"
	worktree := &git.Worktree{}

	// Configure mock expectations
	existingPlugins := map[string]bool{
		"existing/plugin": true,
	}
	composerRunner.EXPECT().GetAllowPlugins(ctx, path).Return(existingPlugins, nil)
	composerRunner.EXPECT().SetConfig(ctx, path, "allow-plugins", "true").Return(nil)

	// Initialize system under test
	ap := NewComposerAllowPlugins(logger, composerRunner)

	// Execute
	e := services.NewPreComposerUpdateEvent(ctx, path, worktree, []string{}, []string{}, false)
	err := ap.preComposerUpdateHandler(e)

	// Verify
	assert.NoError(t, err)
	assert.Equal(t, existingPlugins, ap.allowPlugins)
	composerRunner.AssertExpectations(t)
}

func TestDefaultAllowPlugins_PostComposerUpdateHandler(t *testing.T) {
	// Setup
	logger := zap.NewNop()
	composerRunner := NewMockComposer(t)

	ctx := context.Background()
	path := "/some/path"
	worktree := &git.Worktree{}

	// Setup existing plugins
	existingPlugins := map[string]bool{
		"existing/plugin": true,
	}

	// Configure mock expectations
	allPlugins := map[string]interface{}{
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

	// Initialize system under test
	ap := NewComposerAllowPlugins(logger, composerRunner)
	ap.allowPlugins = existingPlugins

	// Execute
	e := services.NewPostComposerUpdateEvent(ctx, path, worktree)
	err := ap.postComposerUpdateHandler(e)

	// Verify
	assert.NoError(t, err)
	assert.Equal(t, []string{"new/plugin"}, ap.newAllowPlugins)
	composerRunner.AssertExpectations(t)
}

func TestDefaultAllowPlugins_RenderTemplate(t *testing.T) {
	// Setup
	fixture, err := os.ReadFile("testdata/allowplugins.md")
	assert.NoError(t, err, "Failed to read test fixture")

	expected := string(fixture)
	logger := zap.NewNop()
	composerRunner := NewMockComposer(t)

	// Initialize system under test
	ap := NewComposerAllowPlugins(logger, composerRunner)
	ap.newAllowPlugins = []string{"plugin1", "plugin2"}

	// Execute
	result, err := ap.RenderTemplate()

	// Verify
	assert.NoError(t, err)
	assert.Equal(t, expected, result)
}

func TestDefaultAllowPlugins_RenderTemplate_Empty(t *testing.T) {
	logger := zap.NewNop()
	composerRunner := NewMockComposer(t)

	// Initialize system under test
	ap := NewComposerAllowPlugins(logger, composerRunner)
	ap.newAllowPlugins = []string{}

	// Execute
	result, err := ap.RenderTemplate()

	// Verify
	assert.NoError(t, err)
	assert.Equal(t, "", result)
}
