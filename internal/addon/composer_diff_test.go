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
	mockComposer := new(MockComposer)

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

	mockComposer.On("Diff", ctx, testPath, true).Return(expectedDiff, nil)

	// Execute
	err := diff.postComposerUpdateHandler(mockEvent)

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, expectedDiff, diff.table)
	mockComposer.AssertExpectations(t)
}

func TestComposerDiff_RenderTemplate(t *testing.T) {

	fixture, _ := os.ReadFile("testdata/composer_diff.md")
	expected := string(fixture)
	logger := zap.NewNop()

	composerRunner := NewMockComposer(t)
	ap := NewComposerDiff(logger, composerRunner)
	ap.table = "Dummy Table"
	result, err := ap.RenderTemplate()
	assert.NoError(t, err)
	assert.Equal(t, expected, result)
	composerRunner.AssertExpectations(t)

}
