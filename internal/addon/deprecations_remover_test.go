package addon

import (
	"context"
	"testing"

	"github.com/drupdater/drupdater/internal/services"
	"github.com/drupdater/drupdater/pkg/rector"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/gookit/event"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestDeprecationsRemover_SubscribedEvents(t *testing.T) {
	dr := &DeprecationsRemover{}
	events := dr.SubscribedEvents()

	assert.Contains(t, events, "post-code-update")
	item := events["post-code-update"].(event.ListenerItem)
	assert.Equal(t, event.AboveNormal, item.Priority)
}

func TestDeprecationsRemover_RenderTemplate(t *testing.T) {
	dr := &DeprecationsRemover{}
	result, err := dr.RenderTemplate()
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestRemoveDeprecations(t *testing.T) {
	// Common test setup
	logger := zap.NewNop()
	worktree := NewMockWorktree(t)

	t.Run("Rector is not installed", func(t *testing.T) {
		// Setup
		composer := NewMockComposer(t)
		composer.EXPECT().IsPackageInstalled(anyCtx, "/path/to/repo", "palantirnet/drupal-rector").Return(false, assert.AnError)
		composer.EXPECT().Require(anyCtx, "/path/to/repo", []string{"palantirnet/drupal-rector"}).Return("", nil)
		composer.EXPECT().GetCustomCodeDirectories(anyCtx, "/path/to/repo").Return([]string{"web/modules/custom"}, nil)

		runner := NewMockRector(t)
		runner.EXPECT().Run(anyCtx, "/path/to/repo", []string{"web/modules/custom"}).Return(rector.ReturnOutput{
			ChangedFiles: []string{},
			FileDiffs:    []rector.ReturnOutputFillDiff{},
			Totals: rector.ReturnOutputTotals{
				ChangedFiles: 0,
				Errors:       0,
			},
		}, nil)
		composer.EXPECT().Remove(anyCtx, "/path/to/repo", []string{"palantirnet/drupal-rector"}).Return("", nil)

		// Removing the temporarily-required rector left composer.json/composer.lock unchanged
		// from HEAD, so nothing is staged and no cleanup commit happens.
		worktree.EXPECT().AddGlob("composer.*").Return(nil).Once()
		worktree.EXPECT().Status().Return(git.Status{}, nil).Once()

		// Execute
		updateRemoveDeprecations := NewDeprecationsRemover(logger, runner, composer)
		postCodeUpdate := services.NewPostCodeUpdateEvent(context.Background(), "/path/to/repo", worktree)
		err := updateRemoveDeprecations.postCodeUpdateHandler(postCodeUpdate)

		// Assert
		require.NoError(t, err)
		composer.AssertExpectations(t)
		runner.AssertExpectations(t)
		worktree.AssertExpectations(t)
	})

	t.Run("Rector is not installed and removing it leaves a composer.lock diff", func(t *testing.T) {
		composer := NewMockComposer(t)
		composer.EXPECT().IsPackageInstalled(anyCtx, "/path/to/repo", "palantirnet/drupal-rector").Return(false, nil)
		composer.EXPECT().Require(anyCtx, "/path/to/repo", []string{"palantirnet/drupal-rector"}).Return("", nil)
		composer.EXPECT().GetCustomCodeDirectories(anyCtx, "/path/to/repo").Return([]string{"web/modules/custom"}, nil)

		runner := NewMockRector(t)
		runner.EXPECT().Run(anyCtx, "/path/to/repo", []string{"web/modules/custom"}).Return(rector.ReturnOutput{
			ChangedFiles: []string{},
			FileDiffs:    []rector.ReturnOutputFillDiff{},
			Totals:       rector.ReturnOutputTotals{ChangedFiles: 0, Errors: 0},
		}, nil)
		composer.EXPECT().Remove(anyCtx, "/path/to/repo", []string{"palantirnet/drupal-rector"}).Return("", nil)

		wt := NewMockWorktree(t)
		wt.EXPECT().AddGlob("composer.*").Return(nil).Once()
		wt.EXPECT().Status().Return(git.Status{"composer.lock": &git.FileStatus{Staging: git.Modified}}, nil).Once()
		wt.EXPECT().Commit("Remove temporary drupal-rector installation", mock.Anything).Return(plumbing.NewHash(""), nil).Once()

		updateRemoveDeprecations := NewDeprecationsRemover(logger, runner, composer)
		postCodeUpdate := services.NewPostCodeUpdateEvent(context.Background(), "/path/to/repo", wt)
		err := updateRemoveDeprecations.postCodeUpdateHandler(postCodeUpdate)

		require.NoError(t, err)
		composer.AssertExpectations(t)
		runner.AssertExpectations(t)
		wt.AssertExpectations(t)
	})

	t.Run("Rector is installed and command executed successfully with one fix", func(t *testing.T) {
		// Setup
		composer := NewMockComposer(t)
		composer.EXPECT().IsPackageInstalled(anyCtx, "/path/to/repo", "palantirnet/drupal-rector").Return(true, nil)
		composer.EXPECT().GetCustomCodeDirectories(anyCtx, "/path/to/repo").Return([]string{"web/modules/custom"}, nil)

		runner := NewMockRector(t)
		runner.EXPECT().Run(anyCtx, "/path/to/repo", []string{"web/modules/custom"}).Return(rector.ReturnOutput{
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
		updateRemoveDeprecations := NewDeprecationsRemover(logger, runner, composer)
		postCodeUpdate := services.NewPostCodeUpdateEvent(context.Background(), "/path/to/repo", worktree)
		err := updateRemoveDeprecations.postCodeUpdateHandler(postCodeUpdate)

		// Assert
		require.NoError(t, err)
		composer.AssertExpectations(t)
		runner.AssertExpectations(t)
		worktree.AssertExpectations(t)
	})

	t.Run("Rector is installed and command executed successfully without fix", func(t *testing.T) {
		// Setup
		composer := NewMockComposer(t)
		composer.EXPECT().IsPackageInstalled(anyCtx, "/path/to/repo", "palantirnet/drupal-rector").Return(true, nil)
		composer.EXPECT().GetCustomCodeDirectories(anyCtx, "/path/to/repo").Return([]string{"web/modules/custom"}, nil)

		runner := NewMockRector(t)
		runner.EXPECT().Run(anyCtx, "/path/to/repo", []string{"web/modules/custom"}).Return(rector.ReturnOutput{
			ChangedFiles: []string{},
			FileDiffs:    []rector.ReturnOutputFillDiff{},
			Totals: rector.ReturnOutputTotals{
				ChangedFiles: 0,
				Errors:       0,
			},
		}, nil)

		// Execute
		updateRemoveDeprecations := NewDeprecationsRemover(logger, runner, composer)
		postCodeUpdate := services.NewPostCodeUpdateEvent(context.Background(), "/path/to/repo", worktree)
		err := updateRemoveDeprecations.postCodeUpdateHandler(postCodeUpdate)

		// Assert
		require.NoError(t, err)
		composer.AssertExpectations(t)
		runner.AssertExpectations(t)
		worktree.AssertExpectations(t)
	})

	t.Run("Command execution fails", func(t *testing.T) {
		// Setup
		composer := NewMockComposer(t)
		composer.EXPECT().IsPackageInstalled(anyCtx, "/path/to/repo", "palantirnet/drupal-rector").Return(true, nil)
		composer.EXPECT().GetCustomCodeDirectories(anyCtx, "/path/to/repo").Return([]string{"web/modules/custom"}, nil)

		runner := NewMockRector(t)
		runner.EXPECT().Run(anyCtx, "/path/to/repo", []string{"web/modules/custom"}).Return(rector.ReturnOutput{}, assert.AnError)

		// Execute
		updateRemoveDeprecations := NewDeprecationsRemover(logger, runner, composer)
		postCodeUpdate := services.NewPostCodeUpdateEvent(context.Background(), "/path/to/repo", worktree)
		err := updateRemoveDeprecations.postCodeUpdateHandler(postCodeUpdate)

		// Assert
		require.Error(t, err)
		composer.AssertExpectations(t)
		runner.AssertExpectations(t)
	})
}
