package addon

import (
	"testing"

	"github.com/drupdater/drupdater/internal"
	"github.com/drupdater/drupdater/pkg/drush"
	"github.com/drupdater/drupdater/pkg/repo"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.uber.org/zap"
)

func TestUpdateTranslationsEventHandlerWithoutLocaleDeploy(t *testing.T) {
	// Create mocks
	mockDrush := drush.NewMockRunner(t)
	mockRepository := repo.NewMockRepositoryService(t)
	logger := zap.NewNop()

	// Create an instance of UpdateTranslationsEventHandler with mocked dependencies
	handler := NewTranslationsUpdater(logger, mockDrush, mockRepository)

	worktree := internal.NewMockWorktree(t)
	path := "/tmp"
	ctx := t.Context()

	// Set up expectations
	mockDrush.On("IsModuleEnabled", mock.Anything, "/tmp", "example.com", "locale_deploy").Return(false, nil)

	// Verify the results
	event := NewPostSiteUpdateEvent(ctx, path, worktree, "example.com")
	assert.NoError(t, handler.postSiteUpdateHandler(event))

	mockDrush.AssertExpectations(t)
}

func TestUpdateTranslationsEventHandlerWitLocaleDeploy(t *testing.T) {
	// Create mocks
	mockDrush := drush.NewMockRunner(t)
	mockRepository := repo.NewMockRepositoryService(t)
	logger := zap.NewNop()

	// Create an instance of UpdateTranslationsEventHandler with mocked dependencies
	handler := NewTranslationsUpdater(logger, mockDrush, mockRepository)

	worktree := internal.NewMockWorktree(t)
	path := "/tmp"
	ctx := t.Context()

	// Set up expectations
	mockDrush.On("IsModuleEnabled", mock.Anything, "/tmp", "example.com", "locale_deploy").Return(true, nil)
	mockDrush.On("LocalizeTranslations", mock.Anything, "/tmp", "example.com").Return(nil)
	mockDrush.On("GetTranslationPath", mock.Anything, "/tmp", "example.com", true).Return("translations", nil)

	mockRepository.On("IsSomethingStagedInPath", worktree, "translations").Return(true, nil)

	worktree.On("Add", "translations").Return(plumbing.NewHash(""), nil)
	worktree.On("Commit", "Update translations", &git.CommitOptions{}).Return(plumbing.NewHash(""), nil)
	worktree.On("Status").Return(git.Status{}, nil)

	// Verify the results
	event := NewPostSiteUpdateEvent(ctx, path, worktree, "example.com")
	assert.NoError(t, handler.postSiteUpdateHandler(event))

	mockDrush.AssertExpectations(t)
	mockRepository.AssertExpectations(t)
	worktree.AssertExpectations(t)
}
