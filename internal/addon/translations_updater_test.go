package addon

import (
	"context"
	"testing"

	"github.com/drupdater/drupdater/internal/workflow"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestUpdateTranslationsEventHandlerWithoutLocaleDeploy(t *testing.T) {
	// Setup - Create mocks and system under test
	mockDrush := NewMockTranslationsDrush(t)
	logger := zap.NewNop()
	handler := NewTranslationsUpdater(logger, mockDrush)

	committer := &fakeCommitter{}
	path := "/tmp"
	ctx := context.Background()

	mockDrush.EXPECT().IsModuleEnabled(anyCtx, "/tmp", "example.com", "locale_deploy").Return(false, nil)

	event := workflow.NewPostSiteUpdateEvent(ctx, path, committer, "example.com")
	err := handler.postSiteUpdateHandler(event)

	require.NoError(t, err)
	// The recorded result is what reaches the run report. Without it, "skipped because
	// locale_deploy is off" and "ran and changed nothing" are indistinguishable to a reader.
	assert.Equal(t, map[string]TranslationResult{
		"example.com": {Skipped: "locale_deploy not enabled"},
	}, handler.results.All())
	assert.Empty(t, committer.commits)
	mockDrush.AssertExpectations(t)
}

func TestUpdateTranslationsEventHandlerWithLocaleDeploy(t *testing.T) {
	// Setup - Create mocks and system under test
	mockDrush := NewMockTranslationsDrush(t)
	logger := zap.NewNop()
	handler := NewTranslationsUpdater(logger, mockDrush)

	committer := &fakeCommitter{}
	path := "/tmp"
	ctx := context.Background()

	mockDrush.EXPECT().IsModuleEnabled(anyCtx, "/tmp", "example.com", "locale_deploy").Return(true, nil)
	mockDrush.EXPECT().LocalizeTranslations(anyCtx, "/tmp", "example.com").Return(nil)
	mockDrush.EXPECT().GetTranslationPath(anyCtx, "/tmp", "example.com", true).Return("translations", nil)

	event := workflow.NewPostSiteUpdateEvent(ctx, path, committer, "example.com")
	err := handler.postSiteUpdateHandler(event)

	require.NoError(t, err)
	assert.Equal(t, map[string]TranslationResult{
		"example.com": {Path: "translations", Updated: true},
	}, handler.results.All(), "a committed translation update has to be reported as updated")
	assert.Equal(t, []recordedCommit{{message: "Update translations", paths: []string{"translations"}}}, committer.commits)
	mockDrush.AssertExpectations(t)
}

func TestUpdateTranslationsEventHandlerSkipsWhenTranslationPathUnavailable(t *testing.T) {
	// A soft skip, not a fatal error — and nothing committed: an empty path would commit the
	// entire working tree.
	mockDrush := NewMockTranslationsDrush(t)
	logger := zap.NewNop()
	handler := NewTranslationsUpdater(logger, mockDrush)

	committer := &fakeCommitter{}
	path := "/tmp"
	ctx := context.Background()

	mockDrush.EXPECT().IsModuleEnabled(anyCtx, "/tmp", "example.com", "locale_deploy").Return(true, nil)
	mockDrush.EXPECT().LocalizeTranslations(anyCtx, "/tmp", "example.com").Return(nil)
	mockDrush.EXPECT().GetTranslationPath(anyCtx, "/tmp", "example.com", true).Return("", assert.AnError)

	event := workflow.NewPostSiteUpdateEvent(ctx, path, committer, "example.com")
	err := handler.postSiteUpdateHandler(event)

	require.NoError(t, err)
	// The skip reason carries the underlying error, so the report says why translations were
	// left alone rather than silently omitting the site.
	require.Len(t, handler.results.All(), 1)
	assert.Empty(t, handler.results.All()["example.com"].Path)
	assert.False(t, handler.results.All()["example.com"].Updated)
	assert.Contains(t, handler.results.All()["example.com"].Skipped, "translation path not available")
	assert.Contains(t, handler.results.All()["example.com"].Skipped, assert.AnError.Error())
	mockDrush.AssertExpectations(t)
	assert.Empty(t, committer.commits)
}

func TestUpdateTranslationsEventHandlerRecordsNothingToCommit(t *testing.T) {
	// Localized but unchanged: the site still appears in the report, explicitly not updated.
	mockDrush := NewMockTranslationsDrush(t)
	handler := NewTranslationsUpdater(zap.NewNop(), mockDrush)

	committer := &fakeCommitter{unchanged: true}

	mockDrush.EXPECT().IsModuleEnabled(anyCtx, "/tmp", "example.com", "locale_deploy").Return(true, nil)
	mockDrush.EXPECT().LocalizeTranslations(anyCtx, "/tmp", "example.com").Return(nil)
	mockDrush.EXPECT().GetTranslationPath(anyCtx, "/tmp", "example.com", true).Return("translations", nil)

	event := workflow.NewPostSiteUpdateEvent(context.Background(), "/tmp", committer, "example.com")
	require.NoError(t, handler.postSiteUpdateHandler(event))

	assert.Equal(t, map[string]TranslationResult{
		"example.com": {Path: "translations"},
	}, handler.results.All())
}
