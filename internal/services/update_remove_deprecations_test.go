package services

import (
	"testing"

	internal "github.com/drupdater/drupdater/internal"
	"github.com/drupdater/drupdater/pkg/composer"
	"github.com/drupdater/drupdater/pkg/rector"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/stretchr/testify/assert"
	mock "github.com/stretchr/testify/mock"
	"go.uber.org/zap"
)

func TestRemoveDeprecations(t *testing.T) {
	logger := zap.NewNop()
	worktree := internal.NewMockWorktree(t)
	config := internal.Config{
		SkipRector: false,
	}

	t.Run("Rector is not installed", func(t *testing.T) {
		composer := composer.NewMockRunner(t)
		composer.On("IsPackageInstalled", mock.Anything, "/path/to/repo", "palantirnet/drupal-rector").Return(false, assert.AnError)
		composer.On("Require", mock.Anything, "/path/to/repo", "palantirnet/drupal-rector").Return("", nil)
		composer.On("GetCustomCodeDirectories", mock.Anything, "/path/to/repo").Return([]string{"web/modules/custom"}, nil)

		runner := rector.NewMockRunner(t)
		runner.On("Run", mock.Anything, "/path/to/repo", []string{"web/modules/custom"}).Return(rector.ReturnOutput{
			ChangedFiles: []string{},
			FileDiffs:    []rector.ReturnOutputFillDiff{},
			Totals: rector.ReturnOutputTotals{
				ChangedFiles: 0,
				Errors:       0,
			},
		}, nil)
		composer.On("Remove", mock.Anything, "/path/to/repo", "palantirnet/drupal-rector").Return("", nil)

		updateRemoveDeprecations := newUpdateRemoveDeprecations(logger, runner, config, composer)
		err := updateRemoveDeprecations.Execute(t.Context(), "/path/to/repo", worktree)
		assert.NoError(t, err)
		composer.AssertExpectations(t)
		runner.AssertExpectations(t)
		worktree.AssertExpectations(t)
	})

	t.Run("Rector is installed and command executed successfully with one fix", func(t *testing.T) {
		composer := composer.NewMockRunner(t)
		composer.On("IsPackageInstalled", mock.Anything, "/path/to/repo", "palantirnet/drupal-rector").Return(true, nil)
		composer.On("GetCustomCodeDirectories", mock.Anything, "/path/to/repo").Return([]string{"web/modules/custom"}, nil)

		runner := rector.NewMockRunner(t)
		runner.On("Run", mock.Anything, "/path/to/repo", []string{"web/modules/custom"}).Return(rector.ReturnOutput{
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

		worktree.On("Add", "tests/Drupal/FunctionalJavascriptTests/ThunderOrgTestHomePageTest.php").Return(plumbing.NewHash(""), nil)
		worktree.On("Commit", "Remove deprecations", mock.Anything).Return(plumbing.NewHash(""), nil)

		updateRemoveDeprecations := newUpdateRemoveDeprecations(logger, runner, config, composer)
		err := updateRemoveDeprecations.Execute(t.Context(), "/path/to/repo", worktree)
		assert.NoError(t, err)
		composer.AssertExpectations(t)
		runner.AssertExpectations(t)
		worktree.AssertExpectations(t)
	})

	t.Run("Rector is installed and command executed successfully without fix", func(t *testing.T) {
		composer := composer.NewMockRunner(t)
		composer.On("IsPackageInstalled", mock.Anything, "/path/to/repo", "palantirnet/drupal-rector").Return(true, nil)
		composer.On("GetCustomCodeDirectories", mock.Anything, "/path/to/repo").Return([]string{"web/modules/custom"}, nil)

		runner := rector.NewMockRunner(t)
		runner.On("Run", mock.Anything, "/path/to/repo", []string{"web/modules/custom"}).Return(rector.ReturnOutput{
			ChangedFiles: []string{},
			FileDiffs:    []rector.ReturnOutputFillDiff{},
			Totals: rector.ReturnOutputTotals{
				ChangedFiles: 0,
				Errors:       0,
			},
		}, nil)

		updateRemoveDeprecations := newUpdateRemoveDeprecations(logger, runner, config, composer)
		err := updateRemoveDeprecations.Execute(t.Context(), "/path/to/repo", worktree)
		assert.NoError(t, err)
		composer.AssertExpectations(t)
		runner.AssertExpectations(t)
		worktree.AssertExpectations(t)
	})

	t.Run("Command execution fails", func(t *testing.T) {
		composer := composer.NewMockRunner(t)
		composer.On("IsPackageInstalled", mock.Anything, "/path/to/repo", "palantirnet/drupal-rector").Return(true, nil)
		composer.On("GetCustomCodeDirectories", mock.Anything, "/path/to/repo").Return([]string{"web/modules/custom"}, nil)

		runner := rector.NewMockRunner(t)
		runner.On("Run", mock.Anything, "/path/to/repo", []string{"web/modules/custom"}).Return(rector.ReturnOutput{}, assert.AnError)

		updateRemoveDeprecations := newUpdateRemoveDeprecations(logger, runner, config, composer)
		err := updateRemoveDeprecations.Execute(t.Context(), "/path/to/repo", worktree)
		assert.Error(t, err)
		composer.AssertExpectations(t)
		runner.AssertExpectations(t)
	})

}
