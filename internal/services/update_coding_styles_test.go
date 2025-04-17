package services

import (
	"testing"

	internal "github.com/drupdater/drupdater/internal"
	"github.com/drupdater/drupdater/internal/utils"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestCodingStyles(t *testing.T) {
	logger := zap.NewNop()
	worktree := internal.NewMockWorktree(t)

	t.Run("No config file found", func(t *testing.T) {

		fileExists = func(_ string) bool {
			return false
		}

		commandExecutor := utils.NewMockCommandExecutor(t)
		commandExecutor.On("RunPHPCS", t.Context(), "/tmp").Return(`{"totals":{"errors":0,"warnings":0,"fixable":0},"files":{}}`, nil)
		commandExecutor.On("IsPackageInstalled", t.Context(), "/tmp", "drupal/coder").Return(true, nil)
		commandExecutor.On("GetCustomCodeDirectories", t.Context(), "/tmp").Return([]string{"web/modules/custom", "web/themes/custom"}, nil)
		commandExecutor.On("GetInstalledPackageVersion", t.Context(), "/tmp", "drupal/core").Return("9.2.1", nil)

		worktree.On("Add", "phpcs.xml").Return(plumbing.NewHash(""), nil)
		worktree.On("Commit", "Add PHPCS config", &git.CommitOptions{}).Return(plumbing.NewHash(""), nil)

		updateCodingStyles := newUpdateCodingStyles(logger, commandExecutor, internal.Config{SkipCBF: false})
		err := updateCodingStyles.Execute(t.Context(), "/tmp", worktree)
		assert.NoError(t, err)
		commandExecutor.AssertExpectations(t)
	})

	t.Run("No coder found", func(t *testing.T) {

		fileExists = func(_ string) bool {
			return true
		}

		commandExecutor := utils.NewMockCommandExecutor(t)
		commandExecutor.On("RunPHPCS", t.Context(), "/tmp").Return(`{"totals":{"errors":0,"warnings":0,"fixable":0},"files":{}}`, nil)
		commandExecutor.On("IsPackageInstalled", t.Context(), "/tmp", "drupal/coder").Return(false, nil)
		commandExecutor.On("InstallPackages", t.Context(), "/tmp", "--dev", "drupal/coder").Return("", nil)

		worktree.On("AddGlob", "composer.*").Return(nil)
		worktree.On("Commit", "Install drupal/coder", &git.CommitOptions{}).Return(plumbing.NewHash(""), nil)

		updateCodingStyles := newUpdateCodingStyles(logger, commandExecutor, internal.Config{SkipCBF: false})
		err := updateCodingStyles.Execute(t.Context(), "/tmp", worktree)
		assert.NoError(t, err)
		commandExecutor.AssertExpectations(t)
	})

	t.Run("No fixable", func(t *testing.T) {

		fileExists = func(_ string) bool {
			return true
		}

		commandExecutor := utils.NewMockCommandExecutor(t)
		commandExecutor.On("IsPackageInstalled", t.Context(), "/path/to/repo", "drupal/coder").Return(true, nil)
		commandExecutor.On("RunPHPCS", t.Context(), "/path/to/repo").Return(`{"totals":{"errors":0,"warnings":0,"fixable":0},"files":{}}`, nil)

		updateCodingStyles := newUpdateCodingStyles(logger, commandExecutor, internal.Config{SkipCBF: false})
		err := updateCodingStyles.Execute(t.Context(), "/path/to/repo", worktree)
		assert.NoError(t, err)
		commandExecutor.AssertExpectations(t)
	})

	t.Run("Fixable", func(t *testing.T) {

		fileExists = func(_ string) bool {
			return true
		}

		commandExecutor := utils.NewMockCommandExecutor(t)
		commandExecutor.On("IsPackageInstalled", t.Context(), "/path/to/repo", "drupal/coder").Return(true, nil)
		commandExecutor.On("RunPHPCS", t.Context(), "/path/to/repo").Return(`{"totals":{"errors":0,"warnings":1,"fixable":1},"files":{"file1.php":{"errors":0,"warnings":1,"messages":[{"message":"message","source":"source","severity":1,"fixable":true,"type":"type","line":1,"column":1}]}}}`, nil)
		commandExecutor.On("RunPHPCBF", t.Context(), "/path/to/repo").Return(nil)

		worktree.On("Add", "file1.php").Return(plumbing.NewHash(""), nil)
		worktree.On("Commit", "Update coding styles", &git.CommitOptions{}).Return(plumbing.NewHash(""), nil)

		updateCodingStyles := newUpdateCodingStyles(logger, commandExecutor, internal.Config{SkipCBF: false})
		err := updateCodingStyles.Execute(t.Context(), "/path/to/repo", worktree)

		assert.NoError(t, err)
		commandExecutor.AssertExpectations(t)
	})

	t.Run("Fixable error", func(t *testing.T) {

		fileExists = func(_ string) bool {
			return true
		}

		commandExecutor := utils.NewMockCommandExecutor(t)
		commandExecutor.On("IsPackageInstalled", t.Context(), "/path/to/repo", "drupal/coder").Return(true, nil)
		commandExecutor.On("RunPHPCS", t.Context(), "/path/to/repo").Return(`{"totals":{"errors":0,"warnings":1,"fixable":1},"files":{"file1.php":{"errors":0,"warnings":1,"messages":[{"message":"message","source":"source","severity":1,"fixable":true,"type":"type","line":1,"column":1}]}}}`, assert.AnError)

		updateCodingStyles := newUpdateCodingStyles(logger, commandExecutor, internal.Config{SkipCBF: false})
		err := updateCodingStyles.Execute(t.Context(), "/path/to/repo", worktree)

		assert.Error(t, err)
		commandExecutor.AssertExpectations(t)
	})

}
