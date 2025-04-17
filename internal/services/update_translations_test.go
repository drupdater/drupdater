package services

import (
	"testing"

	"github.com/drupdater/drupdater/internal"
	"github.com/drupdater/drupdater/internal/utils"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestUpdateTranslationsEventHandlerWithoutLocaleDeploy(t *testing.T) {
	// Create mocks
	mockCommandExecutor := utils.NewMockCommandExecutor(t)
	mockRepository := NewMockRepositoryService(t)
	logger := zap.NewNop()

	// Create an instance of UpdateTranslationsEventHandler with mocked dependencies
	handler := newUpdateTranslations(logger, mockCommandExecutor, mockRepository)

	worktree := internal.NewMockWorktree(t)

	// Set up expectations
	mockCommandExecutor.On("IsModuleEnabled", "/tmp", "example.com", "locale_deploy").Return(false, nil)

	// Verify the results
	assert.NoError(t, handler.Execute("/tmp", worktree, "example.com"))

	mockCommandExecutor.On("IsModuleEnabled", "/tmp", "example.com", "locale_deploy").Return(true, nil)

	assert.NoError(t, handler.Execute("/tmp", worktree, "example.com"))

	mockCommandExecutor.AssertExpectations(t)

}

func TestUpdateTranslationsEventHandlerWitLocaleDeploy(t *testing.T) {
	// Create mocks
	mockCommandExecutor := utils.NewMockCommandExecutor(t)
	mockRepository := NewMockRepositoryService(t)
	logger := zap.NewNop()

	// Create an instance of UpdateTranslationsEventHandler with mocked dependencies
	handler := newUpdateTranslations(logger, mockCommandExecutor, mockRepository)

	worktree := internal.NewMockWorktree(t)

	// Set up expectations
	mockCommandExecutor.On("IsModuleEnabled", "/tmp", "example.com", "locale_deploy").Return(true, nil)
	mockCommandExecutor.On("LocalizeTranslations", "/tmp", "example.com").Return(nil)
	mockCommandExecutor.On("GetTranslationPath", "/tmp", "example.com", true).Return("translations", nil)

	mockRepository.On("IsSomethingStagedInPath", worktree, "translations").Return(true, nil)

	worktree.On("Add", "translations").Return(plumbing.NewHash(""), nil)
	worktree.On("Commit", "Update translations", &git.CommitOptions{}).Return(plumbing.NewHash(""), nil)
	worktree.On("Status").Return(git.Status{}, nil)

	// Verify the results
	assert.NoError(t, handler.Execute("/tmp", worktree, "example.com"))

	mockCommandExecutor.AssertExpectations(t)

}
