package addon

import (
	"context"
	"testing"

	internal "github.com/drupdater/drupdater/internal"
	"github.com/drupdater/drupdater/internal/services"
	"github.com/drupdater/drupdater/pkg/rector"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.uber.org/zap"
)

func TestRemoveDeprecations(t *testing.T) {
	// Common test setup
	logger := zap.NewNop()
	worktree := NewMockWorktree(t)
	config := internal.Config{
		SkipRector: false,
	}

	t.Run("Rector is not installed", func(t *testing.T) {
		// Setup
		composer := NewMockComposer(t)
		composer.EXPECT().IsPackageInstalled(mock.Anything, "/path/to/repo", "palantirnet/drupal-rector").Return(false, assert.AnError)
		composer.EXPECT().Require(mock.Anything, "/path/to/repo", []string{"palantirnet/drupal-rector"}).Return("", nil)
		composer.EXPECT().GetCustomCodeDirectories(mock.Anything, "/path/to/repo").Return([]string{"web/modules/custom"}, nil)

		runner := NewMockRector(t)
		runner.EXPECT().Run(mock.Anything, "/path/to/repo", []string{"web/modules/custom"}).Return(rector.ReturnOutput{
			ChangedFiles: []string{},
			FileDiffs:    []rector.ReturnOutputFillDiff{},
			Totals: rector.ReturnOutputTotals{
				ChangedFiles: 0,
				Errors:       0,
			},
		}, nil)
		composer.EXPECT().Remove(mock.Anything, "/path/to/repo", []string{"palantirnet/drupal-rector"}).Return("", nil)

		// Execute
		updateRemoveDeprecations := NewDeprecationsRemover(logger, runner, config, composer)
		postCodeUpdate := services.NewPostCodeUpdateEvent(context.Background(), "/path/to/repo", worktree)
		err := updateRemoveDeprecations.postCodeUpdateHandler(postCodeUpdate)

		// Assert
		assert.NoError(t, err)
		composer.AssertExpectations(t)
		runner.AssertExpectations(t)
		worktree.AssertExpectations(t)
	})

	t.Run("Rector is installed and command executed successfully with one fix", func(t *testing.T) {
		// Setup
		composer := NewMockComposer(t)
		composer.EXPECT().IsPackageInstalled(mock.Anything, "/path/to/repo", "palantirnet/drupal-rector").Return(true, nil)
		composer.EXPECT().GetCustomCodeDirectories(mock.Anything, "/path/to/repo").Return([]string{"web/modules/custom"}, nil)

		runner := NewMockRector(t)
		runner.EXPECT().Run(mock.Anything, "/path/to/repo", []string{"web/modules/custom"}).Return(rector.ReturnOutput{
			ChangedFiles: []string{"tests/Drupal/FunctionalJavascriptTests/ThunderOrgTestHomePageTest.php"},
			FileDiffs: []rector.ReturnOutputFillDiff{
				{
					File: "tests/Drupal/FunctionalJavascriptTests/ThunderOrgTestHomePageTest.php",
					Diff: "--- Original\n+++ New\n@@ -13,6 +13,11 @@\n  */\n class ThunderOrgTestHomePageTest extends WebDriverTestBase {\n \n+  /**\n+   * {@inheritdoc}\n+   */\n+  protected $defaultTheme = 'stark';\n+\n   use ThunderTestTrait;\n \n   /**\n",
					AppliedRectors: []string{
						"DrupalRector\\Drupal8\\Rector\\Deprecation\\FunctionalTestDefaultThemePropertyRector",
						"DrupalRector\\Drupal9\\Rector\\Property\\ProtectedStaticModulesPropertyRector",
					},
				},
			},
			Totals: rector.ReturnOutputTotals{
				ChangedFiles: 1,
				Errors:       0,
			},
		}, nil)

		worktree.EXPECT().Add("tests/Drupal/FunctionalJavascriptTests/ThunderOrgTestHomePageTest.php").Return(plumbing.NewHash(""), nil)
		worktree.EXPECT().Commit("Remove deprecations", mock.Anything).Return(plumbing.NewHash(""), nil)

		// Execute
		updateRemoveDeprecations := NewDeprecationsRemover(logger, runner, config, composer)
		postCodeUpdate := services.NewPostCodeUpdateEvent(context.Background(), "/path/to/repo", worktree)
		err := updateRemoveDeprecations.postCodeUpdateHandler(postCodeUpdate)

		// Assert
		assert.NoError(t, err)
		composer.AssertExpectations(t)
		runner.AssertExpectations(t)
		worktree.AssertExpectations(t)
	})

	t.Run("Rector is installed and command executed successfully without fix", func(t *testing.T) {
		// Setup
		composer := NewMockComposer(t)
		composer.EXPECT().IsPackageInstalled(mock.Anything, "/path/to/repo", "palantirnet/drupal-rector").Return(true, nil)
		composer.EXPECT().GetCustomCodeDirectories(mock.Anything, "/path/to/repo").Return([]string{"web/modules/custom"}, nil)

		runner := NewMockRector(t)
		runner.EXPECT().Run(mock.Anything, "/path/to/repo", []string{"web/modules/custom"}).Return(rector.ReturnOutput{
			ChangedFiles: []string{},
			FileDiffs:    []rector.ReturnOutputFillDiff{},
			Totals: rector.ReturnOutputTotals{
				ChangedFiles: 0,
				Errors:       0,
			},
		}, nil)

		// Execute
		updateRemoveDeprecations := NewDeprecationsRemover(logger, runner, config, composer)
		postCodeUpdate := services.NewPostCodeUpdateEvent(context.Background(), "/path/to/repo", worktree)
		err := updateRemoveDeprecations.postCodeUpdateHandler(postCodeUpdate)

		// Assert
		assert.NoError(t, err)
		composer.AssertExpectations(t)
		runner.AssertExpectations(t)
		worktree.AssertExpectations(t)
	})

	t.Run("Command execution fails", func(t *testing.T) {
		// Setup
		composer := NewMockComposer(t)
		composer.EXPECT().IsPackageInstalled(mock.Anything, "/path/to/repo", "palantirnet/drupal-rector").Return(true, nil)
		composer.EXPECT().GetCustomCodeDirectories(mock.Anything, "/path/to/repo").Return([]string{"web/modules/custom"}, nil)

		runner := NewMockRector(t)
		runner.EXPECT().Run(mock.Anything, "/path/to/repo", []string{"web/modules/custom"}).Return(rector.ReturnOutput{}, assert.AnError)

		// Execute
		updateRemoveDeprecations := NewDeprecationsRemover(logger, runner, config, composer)
		postCodeUpdate := services.NewPostCodeUpdateEvent(context.Background(), "/path/to/repo", worktree)
		err := updateRemoveDeprecations.postCodeUpdateHandler(postCodeUpdate)

		// Assert
		assert.Error(t, err)
		composer.AssertExpectations(t)
		runner.AssertExpectations(t)
	})
}
