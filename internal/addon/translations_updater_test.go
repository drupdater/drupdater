package addon

import (
	"context"
	"testing"

	"github.com/drupdater/drupdater/internal/services"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.uber.org/zap"
)

func TestUpdateTranslationsEventHandlerWithoutLocaleDeploy(t *testing.T) {
	// Setup - Create mocks and system under test
	mockDrush := NewMockDrush(t)
	mockRepository := NewMockRepository(t)
	logger := zap.NewNop()
	handler := NewTranslationsUpdater(logger, mockDrush, mockRepository)

	worktree := NewMockWorktree(t)
	path := "/tmp"
	ctx := context.Background()

	// Configure mock expectations
	mockDrush.EXPECT().IsModuleEnabled(mock.Anything, "/tmp", "example.com", "locale_deploy").Return(false, nil)

	// Execute
	event := services.NewPostSiteUpdateEvent(ctx, path, worktree, "example.com")
	err := handler.postSiteUpdateHandler(event)

	// Assert
	assert.NoError(t, err)
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

	// Configure mock expectations
	mockDrush.EXPECT().IsModuleEnabled(mock.Anything, "/tmp", "example.com", "locale_deploy").Return(true, nil)
	mockDrush.EXPECT().LocalizeTranslations(mock.Anything, "/tmp", "example.com").Return(nil)
	mockDrush.EXPECT().GetTranslationPath(mock.Anything, "/tmp", "example.com", true).Return("translations", nil)

	mockRepository.EXPECT().IsSomethingStagedInPath(worktree, "translations").Return(true)

	worktree.EXPECT().Add("translations").Return(plumbing.NewHash(""), nil)
	worktree.EXPECT().Commit("Update translations", &git.CommitOptions{}).Return(plumbing.NewHash(""), nil)
	worktree.EXPECT().Status().Return(git.Status{}, nil)

	// Execute
	event := services.NewPostSiteUpdateEvent(ctx, path, worktree, "example.com")
	err := handler.postSiteUpdateHandler(event)

	// Assert
	assert.NoError(t, err)
	mockDrush.AssertExpectations(t)
	mockRepository.AssertExpectations(t)
	worktree.AssertExpectations(t)
}
