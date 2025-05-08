package addon

import (
	"context"
	"testing"

	"github.com/drupdater/drupdater/internal"
	"github.com/drupdater/drupdater/pkg/composer"
	"github.com/go-git/go-git/v5"
	"github.com/gookit/event"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

// Other composer.Runner methods would be implemented here, but we only need these two for our tests

func TestDefaultComposerNormalize_SubscribedEvents(t *testing.T) {
	logger := zap.NewNop()
	composer := composer.NewMockRunner(t)

	normalize := NewComposerNormalizer(logger, composer)
	events := normalize.SubscribedEvents()

	assert.Contains(t, events, "post-composer-update", "Should subscribe to post-composer-update event")
	assert.Equal(t, event.Min, events["post-composer-update"].(event.ListenerItem).Priority, "Should have maximum priority")
}

func TestDefaultComposerNormalize_PostComposerUpdateHandler_PackageInstalled(t *testing.T) {
	logger := zap.NewNop()
	composer := composer.NewMockRunner(t)

	ctx := context.Background()
	testPath := "/test/path"
	worktree := &git.Worktree{}

	// Mock expectations
	composer.On("IsPackageInstalled", ctx, testPath, "ergebnis/composer-normalize").Return(true, nil)
	composer.On("Normalize", ctx, testPath).Return("normalized", nil)

	normalize := NewComposerNormalizer(logger, composer)

	e := NewPostComposerUpdateEvent(ctx, testPath, worktree, internal.Config{})
	err := normalize.postComposerUpdateHandler(e)

	assert.NoError(t, err)
	composer.AssertExpectations(t)
}

func TestDefaultComposerNormalize_PostComposerUpdateHandler_PackageNotInstalled(t *testing.T) {
	logger := zap.NewNop()
	composer := composer.NewMockRunner(t)

	ctx := context.Background()
	testPath := "/test/path"
	worktree := &git.Worktree{}

	// Mock expectations - package not installed
	composer.On("IsPackageInstalled", ctx, testPath, "ergebnis/composer-normalize").Return(false, nil)

	normalize := NewComposerNormalizer(logger, composer)

	e := NewPostComposerUpdateEvent(ctx, testPath, worktree, internal.Config{})

	err := normalize.postComposerUpdateHandler(e)

	assert.NoError(t, err)
	composer.AssertExpectations(t)
	// Normalize should not be called when package is not installed
	composer.AssertNotCalled(t, "Normalize", ctx, testPath)
}

func TestDefaultComposerNormalize_RenderTemplate(t *testing.T) {
	logger := zap.NewNop()
	composer := composer.NewMockRunner(t)

	normalize := NewComposerNormalizer(logger, composer)

	template, err := normalize.RenderTemplate()

	assert.NoError(t, err)
	assert.Equal(t, "", template, "Template should be empty string")
}
