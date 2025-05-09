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
	logger := zap.NewNop()
	composerRunner := NewMockComposer(t)

	ap := NewComposerAllowPlugins(logger, composerRunner)

	assert.NotNil(t, ap)
	assert.Equal(t, logger, ap.logger)
	assert.Equal(t, composerRunner, ap.composer)
}

func TestDefaultAllowPlugins_SubscribedEvents(t *testing.T) {
	logger := zap.NewNop()
	composerRunner := NewMockComposer(t)

	ap := NewComposerAllowPlugins(logger, composerRunner)
	events := ap.SubscribedEvents()

	assert.Len(t, events, 2)
	assert.Contains(t, events, "pre-composer-update")
	assert.Contains(t, events, "post-composer-update")
}

func TestDefaultAllowPlugins_PreComposerUpdateHandler(t *testing.T) {
	logger := zap.NewNop()
	composerRunner := NewMockComposer(t)

	ctx := context.Background()
	path := "/some/path"
	worktree := &git.Worktree{}

	// Mock the GetAllowPlugins call
	existingPlugins := map[string]bool{
		"existing/plugin": true,
	}
	composerRunner.On("GetAllowPlugins", ctx, path).Return(existingPlugins, nil)

	// Mock the SetConfig call
	composerRunner.On("SetConfig", ctx, path, "allow-plugins", "true").Return(nil)

	ap := NewComposerAllowPlugins(logger, composerRunner)

	e := services.NewPreComposerUpdateEvent(ctx, path, worktree, []string{}, []string{}, false)

	err := ap.preComposerUpdateHandler(e)

	assert.NoError(t, err)
	assert.Equal(t, existingPlugins, ap.allowPlugins)
	composerRunner.AssertExpectations(t)
}

func TestDefaultAllowPlugins_PostComposerUpdateHandler(t *testing.T) {
	logger := zap.NewNop()
	composerRunner := NewMockComposer(t)

	ctx := context.Background()
	path := "/some/path"
	worktree := &git.Worktree{}

	// Setup existing plugins
	existingPlugins := map[string]bool{
		"existing/plugin": true,
	}

	// Mock the GetInstalledPlugins call with existing and new plugins
	allPlugins := map[string]interface{}{
		"existing/plugin": struct{}{},
		"new/plugin":      struct{}{},
	}
	composerRunner.On("GetInstalledPlugins", ctx, path).Return(allPlugins, nil)

	// Mock the SetAllowPlugins call with updated plugins
	expectedUpdatedPlugins := map[string]bool{
		"existing/plugin": true,
		"new/plugin":      false,
	}
	composerRunner.On("SetAllowPlugins", ctx, path, expectedUpdatedPlugins).Return(nil)

	ap := NewComposerAllowPlugins(logger, composerRunner)
	ap.allowPlugins = existingPlugins

	e := services.NewPostComposerUpdateEvent(ctx, path, worktree)
	err := ap.postComposerUpdateHandler(e)

	assert.NoError(t, err)
	assert.Equal(t, []string{"new/plugin"}, ap.newAllowPlugins)
	composerRunner.AssertExpectations(t)
}

func TestDefaultAllowPlugins_RenderTemplate(t *testing.T) {

	fixture, _ := os.ReadFile("testdata/allowplugins.md")
	expected := string(fixture)
	logger := zap.NewNop()

	composerRunner := NewMockComposer(t)
	ap := NewComposerAllowPlugins(logger, composerRunner)
	ap.newAllowPlugins = []string{"plugin1", "plugin2"}
	result, err := ap.RenderTemplate()
	assert.NoError(t, err)
	assert.Equal(t, expected, result)
	composerRunner.AssertExpectations(t)

}
