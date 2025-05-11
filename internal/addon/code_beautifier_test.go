package addon

import (
	"context"
	"testing"

	"github.com/drupdater/drupdater/internal"
	"github.com/drupdater/drupdater/internal/services"
	"github.com/drupdater/drupdater/pkg/phpcs"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.uber.org/zap"
)

func TestCodingStyles(t *testing.T) {
	// Create reusable test dependencies
	logger := zap.NewNop()
	worktree := NewMockWorktree(t)

	// Subtests using table-driven approach
	t.Run("No config file found", func(t *testing.T) {
		// Setup test environment
		fileExists = func(_ string) bool {
			return false
		}

		// Setup mocks with specific expectations
		runner := NewMockPHPCS(t)
		runner.EXPECT().Run(mock.Anything, "/tmp").Return(phpcs.ReturnOutput{
			Totals: phpcs.ReturnOutputTotals{
				Errors:   0,
				Warnings: 0,
				Fixable:  0,
			},
			Files: map[string]phpcs.ReturnOutputFile{},
		}, nil)

		composer := NewMockComposer(t)
		composer.EXPECT().IsPackageInstalled(mock.Anything, "/tmp", "drupal/coder").Return(true, nil)
		composer.EXPECT().GetCustomCodeDirectories(mock.Anything, "/tmp").Return([]string{"web/modules/custom", "web/themes/custom"}, nil)
		composer.EXPECT().GetInstalledPackageVersion(mock.Anything, "/tmp", "drupal/core").Return("9.2.1", nil)

		worktree.EXPECT().Add("phpcs.xml").Return(plumbing.NewHash(""), nil)
		worktree.EXPECT().Commit("Add PHPCS config", &git.CommitOptions{}).Return(plumbing.NewHash(""), nil)

		// Create system under test
		updateCodingStyles := NewCodeBeautifier(logger, runner, internal.Config{SkipCBF: false}, composer)
		postCodeUpdate := services.NewPostCodeUpdateEvent(context.Background(), "/tmp", worktree)

		// Execute and verify
		err := updateCodingStyles.postCodeUpdateHandler(postCodeUpdate)
		assert.NoError(t, err)

		// Verify all expectations were met
		runner.AssertExpectations(t)
		composer.AssertExpectations(t)
	})

	t.Run("No coder found", func(t *testing.T) {

		fileExists = func(_ string) bool {
			return true
		}

		runner := NewMockPHPCS(t)
		runner.EXPECT().Run(mock.Anything, "/tmp").Return(phpcs.ReturnOutput{
			Totals: phpcs.ReturnOutputTotals{
				Errors:   0,
				Warnings: 0,
				Fixable:  0,
			},
			Files: map[string]phpcs.ReturnOutputFile{},
		}, nil)

		composer := NewMockComposer(t)
		composer.EXPECT().IsPackageInstalled(mock.Anything, "/tmp", "drupal/coder").Return(false, nil)
		composer.EXPECT().Require(mock.Anything, "/tmp", []string{"--dev", "drupal/coder"}).Return("", nil)

		worktree.EXPECT().AddGlob("composer.*").Return(nil)
		worktree.EXPECT().Commit("Install drupal/coder", &git.CommitOptions{}).Return(plumbing.NewHash(""), nil)

		updateCodingStyles := NewCodeBeautifier(logger, runner, internal.Config{SkipCBF: false}, composer)
		postCodeUpdate := services.NewPostCodeUpdateEvent(t.Context(), "/tmp", worktree)
		err := updateCodingStyles.postCodeUpdateHandler(postCodeUpdate)
		assert.NoError(t, err)
		runner.AssertExpectations(t)
		composer.AssertExpectations(t)
	})

	t.Run("No fixable", func(t *testing.T) {

		fileExists = func(_ string) bool {
			return true
		}

		runner := NewMockPHPCS(t)
		runner.EXPECT().Run(mock.Anything, "/path/to/repo").Return(phpcs.ReturnOutput{
			Totals: phpcs.ReturnOutputTotals{
				Errors:   0,
				Warnings: 0,
				Fixable:  0,
			},
			Files: map[string]phpcs.ReturnOutputFile{},
		}, nil)
		composer := NewMockComposer(t)
		composer.EXPECT().IsPackageInstalled(mock.Anything, "/path/to/repo", "drupal/coder").Return(true, nil)

		updateCodingStyles := NewCodeBeautifier(logger, runner, internal.Config{SkipCBF: false}, composer)
		postCodeUpdate := services.NewPostCodeUpdateEvent(t.Context(), "/path/to/repo", worktree)
		err := updateCodingStyles.postCodeUpdateHandler(postCodeUpdate)
		assert.NoError(t, err)
		runner.AssertExpectations(t)
		composer.AssertExpectations(t)
	})

	t.Run("Fixable", func(t *testing.T) {

		fileExists = func(_ string) bool {
			return true
		}

		runner := NewMockPHPCS(t)
		runner.EXPECT().Run(mock.Anything, "/path/to/repo").Return(phpcs.ReturnOutput{
			Totals: phpcs.ReturnOutputTotals{
				Errors:   0,
				Warnings: 1,
				Fixable:  1,
			},
			Files: map[string]phpcs.ReturnOutputFile{
				"file1.php": {
					Errors:   0,
					Warnings: 1,
					Messages: []phpcs.ReturnOutputFileMessage{
						{
							Message:  "message",
							Source:   "source",
							Severity: 1,
							Fixable:  true,
							Type:     "type",
							Line:     1,
							Column:   1,
						},
					},
				},
			},
		}, nil)
		runner.EXPECT().RunCBF(mock.Anything, "/path/to/repo").Return(nil)

		composer := NewMockComposer(t)
		composer.EXPECT().IsPackageInstalled(mock.Anything, "/path/to/repo", "drupal/coder").Return(true, nil)

		worktree.EXPECT().Add("file1.php").Return(plumbing.NewHash(""), nil)
		worktree.EXPECT().Commit("Update coding styles", &git.CommitOptions{}).Return(plumbing.NewHash(""), nil)

		updateCodingStyles := NewCodeBeautifier(logger, runner, internal.Config{SkipCBF: false}, composer)
		postCodeUpdate := services.NewPostCodeUpdateEvent(t.Context(), "/path/to/repo", worktree)
		err := updateCodingStyles.postCodeUpdateHandler(postCodeUpdate)

		assert.NoError(t, err)
		runner.AssertExpectations(t)
		composer.AssertExpectations(t)
	})

	t.Run("Fixable error", func(t *testing.T) {

		fileExists = func(_ string) bool {
			return true
		}

		runner := NewMockPHPCS(t)
		runner.EXPECT().Run(mock.Anything, "/path/to/repo").Return(phpcs.ReturnOutput{
			Totals: phpcs.ReturnOutputTotals{
				Errors:   0,
				Warnings: 1,
				Fixable:  1,
			},
			Files: map[string]phpcs.ReturnOutputFile{
				"file1.php": {
					Errors:   0,
					Warnings: 1,
					Messages: []phpcs.ReturnOutputFileMessage{
						{
							Message:  "message",
							Source:   "source",
							Severity: 1,
							Fixable:  true,
							Type:     "type",
							Line:     1,
							Column:   1,
						},
					},
				},
			},
		}, assert.AnError)

		composer := NewMockComposer(t)
		composer.EXPECT().IsPackageInstalled(mock.Anything, "/path/to/repo", "drupal/coder").Return(true, nil)

		updateCodingStyles := NewCodeBeautifier(logger, runner, internal.Config{SkipCBF: false}, composer)
		postCodeUpdate := services.NewPostCodeUpdateEvent(t.Context(), "/path/to/repo", worktree)
		err := updateCodingStyles.postCodeUpdateHandler(postCodeUpdate)

		assert.Error(t, err)
		runner.AssertExpectations(t)
		composer.AssertExpectations(t)
	})

}
