package addon

import (
	"context"
	"testing"

	"github.com/drupdater/drupdater/internal/workflow"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestDefaultComposerNormalize_PostComposerUpdateHandler_PackageInstalled(t *testing.T) {
	logger := zap.NewNop()
	composer := NewMockNormalizerComposer(t)

	ctx := context.Background()
	testPath := "/test/path"
	committer := &fakeCommitter{}

	composer.EXPECT().Project(ctx, testPath).Return(lockedProject("ergebnis/composer-normalize"), nil)
	composer.EXPECT().Normalize(ctx, testPath).Return("normalized", nil)

	normalize := NewComposerNormalizer(logger, composer)

	e := workflow.NewPostComposerUpdateEvent(ctx, testPath, committer)
	err := normalize.postComposerUpdateHandler(e)

	require.NoError(t, err)
	composer.AssertExpectations(t)
}

func TestDefaultComposerNormalize_PostComposerUpdateHandler_PackageNotInstalled(t *testing.T) {
	logger := zap.NewNop()
	composer := NewMockNormalizerComposer(t)

	ctx := context.Background()
	testPath := "/test/path"
	committer := &fakeCommitter{}

	// Configure mock expectations - package not installed
	composer.EXPECT().Project(ctx, testPath).Return(lockedProject(), nil)

	normalize := NewComposerNormalizer(logger, composer)

	e := workflow.NewPostComposerUpdateEvent(ctx, testPath, committer)
	err := normalize.postComposerUpdateHandler(e)

	require.NoError(t, err)
	composer.AssertExpectations(t)
}
