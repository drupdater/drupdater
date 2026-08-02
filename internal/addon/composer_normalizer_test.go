package addon

import (
	"context"
	"testing"

	"github.com/drupdater/drupdater/internal/services"
	"github.com/go-git/go-git/v5"
	"github.com/gookit/event"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestDefaultComposerNormalize_SubscribedEvents(t *testing.T) {
	logger := zap.NewNop()
	composer := NewMockComposer(t)

	normalize := NewComposerNormalizer(logger, composer)
	events := normalize.SubscribedEvents()

	assert.Contains(t, events, "post-composer-update", "Should subscribe to post-composer-update event")
	assert.Equal(t, event.Min, events["post-composer-update"].(event.ListenerItem).Priority, "Should have minimum priority")
}

func TestDefaultComposerNormalize_PostComposerUpdateHandler_PackageInstalled(t *testing.T) {
	logger := zap.NewNop()
	composer := NewMockComposer(t)

	ctx := context.Background()
	testPath := "/test/path"
	worktree := &git.Worktree{}

	composer.EXPECT().IsPackageInstalled(ctx, testPath, "ergebnis/composer-normalize").Return(true, nil)
	composer.EXPECT().Normalize(ctx, testPath).Return("normalized", nil)

	normalize := NewComposerNormalizer(logger, composer)

	e := services.NewPostComposerUpdateEvent(ctx, testPath, worktree)
	err := normalize.postComposerUpdateHandler(e)

	require.NoError(t, err)
	composer.AssertExpectations(t)
}

func TestDefaultComposerNormalize_PostComposerUpdateHandler_PackageNotInstalled(t *testing.T) {
	logger := zap.NewNop()
	composer := NewMockComposer(t)

	ctx := context.Background()
	testPath := "/test/path"
	worktree := &git.Worktree{}

	// Configure mock expectations - package not installed
	composer.EXPECT().IsPackageInstalled(ctx, testPath, "ergebnis/composer-normalize").Return(false, nil)

	normalize := NewComposerNormalizer(logger, composer)

	e := services.NewPostComposerUpdateEvent(ctx, testPath, worktree)
	err := normalize.postComposerUpdateHandler(e)

	require.NoError(t, err)
	composer.AssertExpectations(t)
}

func TestDefaultComposerNormalize_RenderTemplate(t *testing.T) {
	logger := zap.NewNop()
	composer := NewMockComposer(t)
	normalize := NewComposerNormalizer(logger, composer)

	template, err := normalize.RenderTemplate()

	require.NoError(t, err)
	assert.Empty(t, template, "Template should be empty string")
}
