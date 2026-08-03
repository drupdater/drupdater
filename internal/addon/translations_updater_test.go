package addon

import (
	"context"
	"testing"

	"github.com/drupdater/drupdater/internal/services"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/gookit/event"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestTranslationsUpdater_SubscribedEvents(t *testing.T) {
	tu := &TranslationsUpdater{}
	events := tu.SubscribedEvents()

	assert.Contains(t, events, "post-site-update")
	item := events["post-site-update"].(event.ListenerItem)
	assert.Equal(t, event.Normal, item.Priority)
}

func TestTranslationsUpdater_RenderTemplate(t *testing.T) {
	tu := &TranslationsUpdater{}
	result, err := tu.RenderTemplate()
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestUpdateTranslationsEventHandlerWithoutLocaleDeploy(t *testing.T) {
	// Setup - Create mocks and system under test
	mockDrush := NewMockDrush(t)
	mockRepository := NewMockRepository(t)
	logger := zap.NewNop()
	handler := NewTranslationsUpdater(logger, mockDrush, mockRepository)

	worktree := NewMockWorktree(t)
	path := "/tmp"
	ctx := context.Background()

	mockDrush.EXPECT().IsModuleEnabled(anyCtx, "/tmp", "example.com", "locale_deploy").Return(false, nil)

	event := services.NewPostSiteUpdateEvent(ctx, path, worktree, "example.com")
	err := handler.postSiteUpdateHandler(event)

	require.NoError(t, err)
	// The recorded result is what reaches the run report. Without it, "skipped because
	// locale_deploy is off" and "ran and changed nothing" are indistinguishable to a reader.
	assert.Equal(t, map[string]TranslationResult{
		"example.com": {Skipped: "locale_deploy not enabled"},
	}, handler.results)
	mockDrush.AssertExpectations(t)
}

func TestUpdateTranslationsEventHandlerWithLocaleDeploy(t *testing.T) {
	// Setup - Create mocks and system under test
	mockDrush := NewMockDrush(t)
	mockRepository := NewMockRepository(t)
	logger := zap.NewNop()
	handler := NewTranslationsUpdater(logger, mockDrush, mockRepository)

	worktree := NewMockWorktree(t)
	path := "/tmp"
	ctx := context.Background()

	mockDrush.EXPECT().IsModuleEnabled(anyCtx, "/tmp", "example.com", "locale_deploy").Return(true, nil)
	mockDrush.EXPECT().LocalizeTranslations(anyCtx, "/tmp", "example.com").Return(nil)
	mockDrush.EXPECT().GetTranslationPath(anyCtx, "/tmp", "example.com", true).Return("translations", nil)

	mockRepository.EXPECT().IsSomethingStagedInPath(worktree, "translations").Return(true)

	worktree.EXPECT().Add("translations").Return(plumbing.NewHash(""), nil)
	worktree.EXPECT().Commit("Update translations", &git.CommitOptions{}).Return(plumbing.NewHash(""), nil)
	worktree.EXPECT().Status().Return(git.Status{}, nil)

	event := services.NewPostSiteUpdateEvent(ctx, path, worktree, "example.com")
	err := handler.postSiteUpdateHandler(event)

	require.NoError(t, err)
	assert.Equal(t, map[string]TranslationResult{
		"example.com": {Path: "translations", Updated: true},
	}, handler.results, "a committed translation update has to be reported as updated")
	mockDrush.AssertExpectations(t)
	mockRepository.AssertExpectations(t)
	worktree.AssertExpectations(t)
}

func TestUpdateTranslationsEventHandlerSkipsWhenTranslationPathUnavailable(t *testing.T) {
	// A soft skip, not a fatal error — and nothing staged: an empty path handed to
	// Worktree.Add stages the entire working tree.
	mockDrush := NewMockDrush(t)
	mockRepository := NewMockRepository(t)
	logger := zap.NewNop()
	handler := NewTranslationsUpdater(logger, mockDrush, mockRepository)

	worktree := NewMockWorktree(t)
	path := "/tmp"
	ctx := context.Background()

	mockDrush.EXPECT().IsModuleEnabled(anyCtx, "/tmp", "example.com", "locale_deploy").Return(true, nil)
	mockDrush.EXPECT().LocalizeTranslations(anyCtx, "/tmp", "example.com").Return(nil)
	mockDrush.EXPECT().GetTranslationPath(anyCtx, "/tmp", "example.com", true).Return("", assert.AnError)

	event := services.NewPostSiteUpdateEvent(ctx, path, worktree, "example.com")
	err := handler.postSiteUpdateHandler(event)

	require.NoError(t, err)
	// The skip reason carries the underlying error, so the report says why translations were
	// left alone rather than silently omitting the site.
	require.Len(t, handler.results, 1)
	assert.Empty(t, handler.results["example.com"].Path)
	assert.False(t, handler.results["example.com"].Updated)
	assert.Contains(t, handler.results["example.com"].Skipped, "translation path not available")
	assert.Contains(t, handler.results["example.com"].Skipped, assert.AnError.Error())
	mockDrush.AssertExpectations(t)
	worktree.AssertNotCalled(t, "Add", mock.Anything)
	worktree.AssertNotCalled(t, "Commit", mock.Anything, mock.Anything)
}

func TestUpdateTranslationsEventHandlerRecordsNothingToCommit(t *testing.T) {
	// Localized but unchanged: the site still appears in the report, explicitly not updated.
	mockDrush := NewMockDrush(t)
	mockRepository := NewMockRepository(t)
	handler := NewTranslationsUpdater(zap.NewNop(), mockDrush, mockRepository)

	worktree := NewMockWorktree(t)

	mockDrush.EXPECT().IsModuleEnabled(anyCtx, "/tmp", "example.com", "locale_deploy").Return(true, nil)
	mockDrush.EXPECT().LocalizeTranslations(anyCtx, "/tmp", "example.com").Return(nil)
	mockDrush.EXPECT().GetTranslationPath(anyCtx, "/tmp", "example.com", true).Return("translations", nil)
	worktree.EXPECT().Add("translations").Return(plumbing.NewHash(""), nil)
	worktree.EXPECT().Status().Return(git.Status{}, nil)
	mockRepository.EXPECT().IsSomethingStagedInPath(worktree, "translations").Return(false)

	event := services.NewPostSiteUpdateEvent(context.Background(), "/tmp", worktree, "example.com")
	require.NoError(t, handler.postSiteUpdateHandler(event))

	assert.Equal(t, map[string]TranslationResult{
		"example.com": {Path: "translations"},
	}, handler.results)
	worktree.AssertNotCalled(t, "Commit", mock.Anything, mock.Anything)
}
