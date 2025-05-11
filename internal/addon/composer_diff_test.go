package addon

import (
	"context"
	"os"
	"testing"

	"github.com/drupdater/drupdater/internal/services"
	"github.com/gookit/event"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestNewComposerDiff(t *testing.T) {
	// Setup
	logger := zap.NewNop()
	mockComposer := NewMockComposer(t)

	// Execute
	diff := NewComposerDiff(logger, mockComposer)

	// Assert
	assert.NotNil(t, diff)
	assert.Equal(t, logger, diff.logger)
	assert.Equal(t, mockComposer, diff.composer)
}

func TestComposerDiff_SubscribedEvents(t *testing.T) {
	// Setup
	diff := &ComposerDiff{}

	// Execute
	events := diff.SubscribedEvents()

	// Assert
	assert.Contains(t, events, "post-composer-update")
	assert.IsType(t, event.ListenerItem{}, events["post-composer-update"])
}

func TestComposerDiff_PostComposerUpdateHandler_Success(t *testing.T) {
	// Setup
	logger := zap.NewNop()
	mockComposer := NewMockComposer(t)
	diff := NewComposerDiff(logger, mockComposer)

	ctx := context.Background()
	testPath := "/test/path"
	expectedDiff := "package diff table"

	mockEvent := services.NewPostComposerUpdateEvent(ctx, testPath, nil)

	// Configure mock expectations
	mockComposer.EXPECT().Diff(ctx, testPath, true).Return(expectedDiff, nil)

	// Execute
	err := diff.postComposerUpdateHandler(mockEvent)

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, expectedDiff, diff.table)
	mockComposer.AssertExpectations(t)
}

func TestComposerDiff_RenderTemplate(t *testing.T) {
	// Setup - Read expected output from fixture file
	fixture, err := os.ReadFile("testdata/composer_diff.md")
	assert.NoError(t, err, "Failed to read test fixture")

	expected := string(fixture)
	logger := zap.NewNop()

	// Create mock and system under test
	composerRunner := NewMockComposer(t)
	composerDiff := NewComposerDiff(logger, composerRunner)
	composerDiff.table = "Dummy Table"

	// Execute
	result, err := composerDiff.RenderTemplate()

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, expected, result)
	composerRunner.AssertExpectations(t)
}
