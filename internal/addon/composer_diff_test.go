package addon

import (
	"testing"

	"github.com/drupdater/drupdater/internal/composer"
	"github.com/drupdater/drupdater/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestNewComposerDiff(t *testing.T) {
	logger := zap.NewNop()
	mockComposer := NewMockDiffComposer(t)

	diff := NewComposerDiff(logger, mockComposer)

	assert.NotNil(t, diff)
	assert.Equal(t, logger, diff.logger)
	assert.Equal(t, mockComposer, diff.composer)
}

func TestComposerDiffRecordsAndLogsTheChanges(t *testing.T) {
	core, logs := observer.New(zap.InfoLevel)
	mockComposer := NewMockDiffComposer(t)
	entries := []composer.DiffEntry{
		{Package: "drupal/core", Operation: "upgrade", From: "10.3.8", To: "10.3.9", CompareURL: "https://example.com/compare"},
		{Package: "drupal/redirect", Operation: "install", To: "1.10.0"},
		{Package: "phpunit/phpunit", Operation: "remove", From: "10.5.1", Dev: true},
	}
	mockComposer.EXPECT().Diff(anyCtx, "/test/path").Return(entries, nil)

	cd := NewComposerDiff(zap.New(core), mockComposer)
	found, err := cd.postComposerUpdateHandler(workflow.NewPostComposerUpdateEvent(t.Context(), "/test/path", nil))
	require.NoError(t, err)

	assert.Equal(t, entries, found)
	require.Equal(t, 1, logs.Len())
	assert.Equal(t, "dependency diff\n"+
		"upgrade drupal/core: 10.3.8 → 10.3.9\n"+
		"install drupal/redirect: - → 1.10.0\n"+
		"remove phpunit/phpunit (dev): 10.5.1 → -\n", logs.All()[0].Message)
}

func TestComposerDiffLogsASingleChange(t *testing.T) {
	core, logs := observer.New(zap.InfoLevel)
	mockComposer := NewMockDiffComposer(t)
	mockComposer.EXPECT().Diff(anyCtx, "/test/path").Return([]composer.DiffEntry{
		{Package: "drupal/core", Operation: "change", From: "dev-main 1", To: "dev-main 2"},
	}, nil)

	cd := NewComposerDiff(zap.New(core), mockComposer)
	_, err := cd.postComposerUpdateHandler(workflow.NewPostComposerUpdateEvent(t.Context(), "/test/path", nil))
	require.NoError(t, err)

	require.Equal(t, 1, logs.Len())
	assert.Equal(t, "dependency diff\nchange drupal/core: dev-main 1 → dev-main 2\n", logs.All()[0].Message)
}

func TestComposerDiffLogsNothingWithoutChanges(t *testing.T) {
	core, logs := observer.New(zap.InfoLevel)
	mockComposer := NewMockDiffComposer(t)
	mockComposer.EXPECT().Diff(anyCtx, "/test/path").Return(nil, nil)

	cd := NewComposerDiff(zap.New(core), mockComposer)
	_, err := cd.postComposerUpdateHandler(workflow.NewPostComposerUpdateEvent(t.Context(), "/test/path", nil))
	require.NoError(t, err)

	assert.Zero(t, logs.Len())
}
