package addon

import (
	"testing"

	"github.com/drupdater/drupdater/internal/workflow"
	"github.com/drupdater/drupdater/pkg/composer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestNewComposerDiff(t *testing.T) {
	logger := zap.NewNop()
	mockComposer := NewMockComposer(t)

	diff := NewComposerDiff(logger, mockComposer)

	assert.NotNil(t, diff)
	assert.Equal(t, logger, diff.logger)
	assert.Equal(t, mockComposer, diff.composer)
}

func TestComposerDiffRecordsAndLogsTheChanges(t *testing.T) {
	core, logs := observer.New(zap.InfoLevel)
	mockComposer := NewMockComposer(t)
	entries := []composer.DiffEntry{
		{Package: "drupal/core", Operation: "upgrade", From: "10.3.8", To: "10.3.9", CompareURL: "https://example.com/compare"},
		{Package: "drupal/redirect", Operation: "install", To: "1.10.0"},
		{Package: "phpunit/phpunit", Operation: "remove", From: "10.5.1", Dev: true},
	}
	mockComposer.EXPECT().Diff(anyCtx, "/test/path").Return(entries, nil)

	cd := NewComposerDiff(zap.New(core), mockComposer)
	require.NoError(t, cd.postComposerUpdateHandler(workflow.NewPostComposerUpdateEvent(t.Context(), "/test/path", nil)))

	assert.Equal(t, entries, cd.entries)
	require.Equal(t, 1, logs.Len())
	assert.Equal(t, "dependency diff\n"+
		"upgrade drupal/core: 10.3.8 → 10.3.9\n"+
		"install drupal/redirect: - → 1.10.0\n"+
		"remove phpunit/phpunit (dev): 10.5.1 → -\n", logs.All()[0].Message)
}

func TestComposerDiffLogsNothingWithoutChanges(t *testing.T) {
	core, logs := observer.New(zap.InfoLevel)
	mockComposer := NewMockComposer(t)
	mockComposer.EXPECT().Diff(anyCtx, "/test/path").Return(nil, nil)

	cd := NewComposerDiff(zap.New(core), mockComposer)
	require.NoError(t, cd.postComposerUpdateHandler(workflow.NewPostComposerUpdateEvent(t.Context(), "/test/path", nil)))

	assert.Zero(t, logs.Len())
	assert.Nil(t, cd.ReportData())
}
