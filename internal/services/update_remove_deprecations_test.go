package services

import (
	"testing"

	internal "ebersolve.com/updater/internal"
	"ebersolve.com/updater/internal/utils"
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

		commandExecutor := utils.NewMockCommandExecutor(t)
		commandExecutor.On("IsPackageInstalled", "/path/to/repo", "palantirnet/drupal-rector").Return(false, assert.AnError)
		commandExecutor.On("InstallPackages", "/path/to/repo", "palantirnet/drupal-rector").Return("", nil)
		commandExecutor.On("RunRector", "/path/to/repo").Return(`{
    "totals": {
        "changed_files": 0,
        "errors": 0
    },
    "file_diffs": [],
    "changed_files": []
}`, nil)
		commandExecutor.On("RemovePackages", "/path/to/repo", "palantirnet/drupal-rector").Return("", nil)

		updateRemoveDeprecations := newUpdateRemoveDeprecations(logger, commandExecutor, config)
		err := updateRemoveDeprecations.Execute("/path/to/repo", worktree)
		assert.NoError(t, err)
		commandExecutor.AssertExpectations(t)
		worktree.AssertExpectations(t)
	})

	t.Run("Rector is installed and command executed successfully with one fix", func(t *testing.T) {

		commandExecutor := utils.NewMockCommandExecutor(t)

		commandExecutor.On("IsPackageInstalled", "/path/to/repo", "palantirnet/drupal-rector").Return(true, nil)
		commandExecutor.On("RunRector", "/path/to/repo").Return(`{
    "totals": {
        "changed_files": 1,
        "errors": 0
    },
    "file_diffs": [
        {
            "file": "tests/Drupal/FunctionalJavascriptTests/ThunderOrgTestHomePageTest.php",
            "diff": "--- Original\n+++ New\n@@ -13,6 +13,11 @@\n  */\n class ThunderOrgTestHomePageTest extends WebDriverTestBase {\n \n+  /**\n+   * {@inheritdoc}\n+   */\n+  protected $defaultTheme = 'stark';\n+\n   use ThunderTestTrait;\n \n   /**\n",
            "applied_rectors": [
                "DrupalRector\\Drupal8\\Rector\\Deprecation\\FunctionalTestDefaultThemePropertyRector",
                "DrupalRector\\Drupal9\\Rector\\Property\\ProtectedStaticModulesPropertyRector"
            ]
        }
    ],
    "changed_files": [
        "tests/Drupal/FunctionalJavascriptTests/ThunderOrgTestHomePageTest.php"
    ]
}`, nil)
		worktree.On("Add", "tests/Drupal/FunctionalJavascriptTests/ThunderOrgTestHomePageTest.php").Return(plumbing.NewHash(""), nil)
		worktree.On("Commit", "Remove deprecations", mock.Anything).Return(plumbing.NewHash(""), nil)

		updateRemoveDeprecations := newUpdateRemoveDeprecations(logger, commandExecutor, config)
		err := updateRemoveDeprecations.Execute("/path/to/repo", worktree)
		assert.NoError(t, err)
		commandExecutor.AssertExpectations(t)
		worktree.AssertExpectations(t)
	})

	t.Run("Rector is installed and command executed successfully without fix", func(t *testing.T) {

		commandExecutor := utils.NewMockCommandExecutor(t)

		commandExecutor.On("IsPackageInstalled", "/path/to/repo", "palantirnet/drupal-rector").Return(true, nil)
		commandExecutor.On("RunRector", "/path/to/repo").Return(`{
    "totals": {
        "changed_files": 0,
        "errors": 0
    },
    "file_diffs": [],
    "changed_files": []
}`, nil)

		updateRemoveDeprecations := newUpdateRemoveDeprecations(logger, commandExecutor, config)
		err := updateRemoveDeprecations.Execute("/path/to/repo", worktree)
		assert.NoError(t, err)
		commandExecutor.AssertExpectations(t)
		worktree.AssertExpectations(t)
	})

	t.Run("Command execution fails", func(t *testing.T) {
		commandExecutor := utils.NewMockCommandExecutor(t)

		commandExecutor.On("IsPackageInstalled", "/path/to/repo", "palantirnet/drupal-rector").Return(true, nil)
		commandExecutor.On("RunRector", "/path/to/repo").Return("", assert.AnError)

		updateRemoveDeprecations := newUpdateRemoveDeprecations(logger, commandExecutor, config)
		err := updateRemoveDeprecations.Execute("/path/to/repo", worktree)
		assert.Error(t, err)
		commandExecutor.AssertExpectations(t)
	})

}
