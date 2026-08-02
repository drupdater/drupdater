package addon

import (
	"context"
	"os"
	"testing"

	"github.com/drupdater/drupdater/internal/services"
	"github.com/gookit/event"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestNewComposerDiff(t *testing.T) {
	logger := zap.NewNop()
	mockComposer := NewMockComposer(t)

	diff := NewComposerDiff(logger, mockComposer)

	assert.NotNil(t, diff)
	assert.Equal(t, logger, diff.logger)
	assert.Equal(t, mockComposer, diff.composer)
}

func TestComposerDiff_SubscribedEvents(t *testing.T) {
	diff := &ComposerDiff{}

	events := diff.SubscribedEvents()

	assert.Contains(t, events, "post-composer-update")
	assert.IsType(t, event.ListenerItem{}, events["post-composer-update"])
}

func TestComposerDiff_PostComposerUpdateHandler_Success(t *testing.T) {
	logger := zap.NewNop()
	mockComposer := NewMockComposer(t)
	diff := NewComposerDiff(logger, mockComposer)

	ctx := context.Background()
	testPath := "/test/path"
	expectedDiff := "package diff table"

	mockEvent := services.NewPostComposerUpdateEvent(ctx, testPath, nil)

	mockComposer.EXPECT().Diff(ctx, testPath, true).Return(expectedDiff, nil)
	mockComposer.EXPECT().Diff(ctx, testPath, false).Return("plain text diff", nil)

	err := diff.postComposerUpdateHandler(mockEvent)

	require.NoError(t, err)
	assert.Equal(t, expectedDiff, diff.table)
	mockComposer.AssertExpectations(t)
}

func TestComposerDiff_RenderTemplate(t *testing.T) {
	// Setup - Read expected output from fixture file
	fixture, err := os.ReadFile("testdata/composer_diff.md")
	require.NoError(t, err, "Failed to read test fixture")

	expected := string(fixture)
	logger := zap.NewNop()

	composerRunner := NewMockComposer(t)
	composerDiff := NewComposerDiff(logger, composerRunner)
	composerDiff.table = "Dummy Table"

	result, err := composerDiff.RenderTemplate()

	require.NoError(t, err)
	assert.Equal(t, expected, result)
	composerRunner.AssertExpectations(t)
}

func TestComposerDiff_RenderTemplate_NoDiff(t *testing.T) {
	// An empty table (postComposerUpdateHandler never ran) must render nothing, not an empty
	// "Dependency updates" header.
	diff := NewComposerDiff(zap.NewNop(), NewMockComposer(t))

	result, err := diff.RenderTemplate()

	require.NoError(t, err)
	assert.Empty(t, result)
}
