package services

import (
	"testing"

	internal "github.com/drupdater/drupdater/internal"
	"github.com/drupdater/drupdater/pkg/composer"
	"github.com/drupdater/drupdater/pkg/phpcs"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.uber.org/zap"
)

func TestCodingStyles(t *testing.T) {
	logger := zap.NewNop()
	worktree := internal.NewMockWorktree(t)

	t.Run("No config file found", func(t *testing.T) {

		fileExists = func(_ string) bool {
			return false
		}

		phpcs := phpcs.NewMockPhpCsService(t)
		phpcs.On("Run", mock.Anything, "/tmp").Return(`{"totals":{"errors":0,"warnings":0,"fixable":0},"files":{}}`, nil)

		composer := composer.NewMockComposerService(t)
		composer.On("IsPackageInstalled", mock.Anything, "/tmp", "drupal/coder").Return(true, nil)
		composer.On("GetCustomCodeDirectories", mock.Anything, "/tmp").Return([]string{"web/modules/custom", "web/themes/custom"}, nil)
		composer.On("GetInstalledPackageVersion", mock.Anything, "/tmp", "drupal/core").Return("9.2.1", nil)

		worktree.On("Add", "phpcs.xml").Return(plumbing.NewHash(""), nil)
		worktree.On("Commit", "Add PHPCS config", &git.CommitOptions{}).Return(plumbing.NewHash(""), nil)

		updateCodingStyles := newUpdateCodingStyles(logger, phpcs, internal.Config{SkipCBF: false}, composer)
		err := updateCodingStyles.Execute(t.Context(), "/tmp", worktree)
		assert.NoError(t, err)
		phpcs.AssertExpectations(t)
		composer.AssertExpectations(t)
	})

	t.Run("No coder found", func(t *testing.T) {

		fileExists = func(_ string) bool {
			return true
		}

		phpcs := phpcs.NewMockPhpCsService(t)
		phpcs.On("Run", mock.Anything, "/tmp").Return(`{"totals":{"errors":0,"warnings":0,"fixable":0},"files":{}}`, nil)

		composer := composer.NewMockComposerService(t)
		composer.On("IsPackageInstalled", mock.Anything, "/tmp", "drupal/coder").Return(false, nil)
		composer.On("Require", mock.Anything, "/tmp", "--dev", "drupal/coder").Return("", nil)

		worktree.On("AddGlob", "composer.*").Return(nil)
		worktree.On("Commit", "Install drupal/coder", &git.CommitOptions{}).Return(plumbing.NewHash(""), nil)

		updateCodingStyles := newUpdateCodingStyles(logger, phpcs, internal.Config{SkipCBF: false}, composer)
		err := updateCodingStyles.Execute(t.Context(), "/tmp", worktree)
		assert.NoError(t, err)
		phpcs.AssertExpectations(t)
		composer.AssertExpectations(t)
	})

	t.Run("No fixable", func(t *testing.T) {

		fileExists = func(_ string) bool {
			return true
		}

		phpcs := phpcs.NewMockPhpCsService(t)
		phpcs.On("Run", mock.Anything, "/path/to/repo").Return(`{"totals":{"errors":0,"warnings":0,"fixable":0},"files":{}}`, nil)

		composer := composer.NewMockComposerService(t)
		composer.On("IsPackageInstalled", mock.Anything, "/path/to/repo", "drupal/coder").Return(true, nil)

		updateCodingStyles := newUpdateCodingStyles(logger, phpcs, internal.Config{SkipCBF: false}, composer)
		err := updateCodingStyles.Execute(t.Context(), "/path/to/repo", worktree)
		assert.NoError(t, err)
		phpcs.AssertExpectations(t)
		composer.AssertExpectations(t)
	})

	t.Run("Fixable", func(t *testing.T) {

		fileExists = func(_ string) bool {
			return true
		}

		phpcs := phpcs.NewMockPhpCsService(t)
		phpcs.On("Run", mock.Anything, "/path/to/repo").Return(`{"totals":{"errors":0,"warnings":1,"fixable":1},"files":{"file1.php":{"errors":0,"warnings":1,"messages":[{"message":"message","source":"source","severity":1,"fixable":true,"type":"type","line":1,"column":1}]}}}`, nil)
		phpcs.On("RunCBF", mock.Anything, "/path/to/repo").Return(nil)

		composer := composer.NewMockComposerService(t)
		composer.On("IsPackageInstalled", mock.Anything, "/path/to/repo", "drupal/coder").Return(true, nil)

		worktree.On("Add", "file1.php").Return(plumbing.NewHash(""), nil)
		worktree.On("Commit", "Update coding styles", &git.CommitOptions{}).Return(plumbing.NewHash(""), nil)

		updateCodingStyles := newUpdateCodingStyles(logger, phpcs, internal.Config{SkipCBF: false}, composer)
		err := updateCodingStyles.Execute(t.Context(), "/path/to/repo", worktree)

		assert.NoError(t, err)
		phpcs.AssertExpectations(t)
		composer.AssertExpectations(t)
	})

	t.Run("Fixable error", func(t *testing.T) {

		fileExists = func(_ string) bool {
			return true
		}

		phpcs := phpcs.NewMockPhpCsService(t)
		phpcs.On("Run", mock.Anything, "/path/to/repo").Return(`{"totals":{"errors":0,"warnings":1,"fixable":1},"files":{"file1.php":{"errors":0,"warnings":1,"messages":[{"message":"message","source":"source","severity":1,"fixable":true,"type":"type","line":1,"column":1}]}}}`, assert.AnError)

		composer := composer.NewMockComposerService(t)
		composer.On("IsPackageInstalled", mock.Anything, "/path/to/repo", "drupal/coder").Return(true, nil)

		updateCodingStyles := newUpdateCodingStyles(logger, phpcs, internal.Config{SkipCBF: false}, composer)
		err := updateCodingStyles.Execute(t.Context(), "/path/to/repo", worktree)

		assert.Error(t, err)
		phpcs.AssertExpectations(t)
		composer.AssertExpectations(t)
	})

}
