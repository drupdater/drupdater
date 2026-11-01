package addon

import (
	"testing"

	"github.com/drupdater/drupdater/internal/composer"
	"github.com/drupdater/drupdater/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestDefaultComposerNormalize_PostComposerUpdateHandler_PackageInstalled(t *testing.T) {
	logger := zap.NewNop()
	composer := NewMockNormalizerComposer(t)

	ctx := t.Context()
	testPath := "/test/path"
	committer := &fakeCommitter{}

	composer.EXPECT().Project(ctx, testPath).Return(lockedProject("ergebnis/composer-normalize"), nil)
	composer.EXPECT().Normalize(ctx, testPath).Return("normalized", nil)

	normalize := NewComposerNormalizer(logger, composer)

	e := workflow.NewPostComposerUpdateEvent(ctx, testPath, committer)
	err := normalize.postComposerUpdateHandler(e)

	require.NoError(t, err)
}

func TestDefaultComposerNormalize_PostComposerUpdateHandler_PackageNotInstalled(t *testing.T) {
	logger := zap.NewNop()
	composer := NewMockNormalizerComposer(t)

	ctx := t.Context()
	testPath := "/test/path"
	committer := &fakeCommitter{}

	// Configure mock expectations - package not installed
	composer.EXPECT().Project(ctx, testPath).Return(lockedProject(), nil)

	normalize := NewComposerNormalizer(logger, composer)

	e := workflow.NewPostComposerUpdateEvent(ctx, testPath, committer)
	err := normalize.postComposerUpdateHandler(e)

	require.NoError(t, err)
}

func TestComposerNormalizerErrors(t *testing.T) {
	t.Run("install check fails", func(t *testing.T) {
		composerService := NewMockNormalizerComposer(t)
		composerService.EXPECT().Project(anyCtx, "/tmp").Return(composer.Project{}, assert.AnError)

		cn := NewComposerNormalizer(zap.NewNop(), composerService)
		require.Error(t, cn.postComposerUpdateHandler(workflow.NewPostComposerUpdateEvent(t.Context(), "/tmp", nil)))
	})

	t.Run("normalize fails", func(t *testing.T) {
		composerService := NewMockNormalizerComposer(t)
		composerService.EXPECT().Project(anyCtx, "/tmp").Return(lockedProject("ergebnis/composer-normalize"), nil)
		composerService.EXPECT().Normalize(anyCtx, "/tmp").Return("", assert.AnError)

		cn := NewComposerNormalizer(zap.NewNop(), composerService)
		err := cn.postComposerUpdateHandler(workflow.NewPostComposerUpdateEvent(t.Context(), "/tmp", nil))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to normalize composer.json")
	})
}
