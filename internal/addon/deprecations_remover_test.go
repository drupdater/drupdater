package addon

import (
	"context"
	"testing"

	"github.com/drupdater/drupdater/internal/workflow"
	"github.com/drupdater/drupdater/pkg/rector"

	"github.com/gookit/event"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// recordFixes sorts each file's rules, which in place would reorder data the caller still owns.
func TestRecordFixes_DoesNotSortRectorsOutputInPlace(t *testing.T) {
	output := rector.ReturnOutput{
		ChangedFiles: []string{"web/modules/custom/a.php"},
		FileDiffs: []rector.ReturnOutputFillDiff{
			{File: "web/modules/custom/a.php", AppliedRectors: []string{"ZRector", "ARector"}},
		},
	}

	dr := &DeprecationsRemover{}
	dr.recordFixes(output)

	assert.Equal(t, []string{"ZRector", "ARector"}, output.FileDiffs[0].AppliedRectors,
		"rector's own output was reordered")
	assert.Equal(t, []string{"ARector", "ZRector"}, dr.fixes[0].AppliedRectors,
		"the report section is still sorted")
}

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

	t.Run("Rector is not installed", func(t *testing.T) {
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

		// Whatever removing the temporary rector left in composer.* gets its own commit.
		committer := &fakeCommitter{unchanged: true}
		updateRemoveDeprecations := NewDeprecationsRemover(logger, runner, composer)
		postCodeUpdate := workflow.NewPostCodeUpdateEvent(context.Background(), "/path/to/repo", committer)
		err := updateRemoveDeprecations.postCodeUpdateHandler(postCodeUpdate)

		require.NoError(t, err)
		assert.Equal(t, []recordedCommit{
			{message: "Remove temporary drupal-rector installation", paths: []string{"composer.json", "composer.lock"}},
		}, committer.commits)
		composer.AssertExpectations(t)
		runner.AssertExpectations(t)
	})

	t.Run("a failed cleanup commit fails the handler", func(t *testing.T) {
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

		committer := &fakeCommitter{err: assert.AnError}
		updateRemoveDeprecations := NewDeprecationsRemover(logger, runner, composer)
		postCodeUpdate := workflow.NewPostCodeUpdateEvent(context.Background(), "/path/to/repo", committer)
		err := updateRemoveDeprecations.postCodeUpdateHandler(postCodeUpdate)

		require.ErrorIs(t, err, assert.AnError)
		composer.AssertExpectations(t)
		runner.AssertExpectations(t)
	})

	t.Run("Rector is installed and command executed successfully with one fix", func(t *testing.T) {
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

		committer := &fakeCommitter{}
		updateRemoveDeprecations := NewDeprecationsRemover(logger, runner, composer)
		postCodeUpdate := workflow.NewPostCodeUpdateEvent(context.Background(), "/path/to/repo", committer)
		err := updateRemoveDeprecations.postCodeUpdateHandler(postCodeUpdate)

		require.NoError(t, err)
		assert.Equal(t, []recordedCommit{
			{message: "Remove deprecations", paths: []string{"tests/Drupal/FunctionalJavascriptTests/ThunderOrgTestHomePageTest.php"}},
		}, committer.commits)
		composer.AssertExpectations(t)
		runner.AssertExpectations(t)
	})

	t.Run("Rector is installed and command executed successfully without fix", func(t *testing.T) {
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

		committer := &fakeCommitter{}
		updateRemoveDeprecations := NewDeprecationsRemover(logger, runner, composer)
		postCodeUpdate := workflow.NewPostCodeUpdateEvent(context.Background(), "/path/to/repo", committer)
		err := updateRemoveDeprecations.postCodeUpdateHandler(postCodeUpdate)

		require.NoError(t, err)
		assert.Empty(t, committer.commits)
		composer.AssertExpectations(t)
		runner.AssertExpectations(t)
	})

	t.Run("Command execution fails", func(t *testing.T) {
		composer := NewMockComposer(t)
		composer.EXPECT().IsPackageInstalled(anyCtx, "/path/to/repo", "palantirnet/drupal-rector").Return(true, nil)
		composer.EXPECT().GetCustomCodeDirectories(anyCtx, "/path/to/repo").Return([]string{"web/modules/custom"}, nil)

		runner := NewMockRector(t)
		runner.EXPECT().Run(anyCtx, "/path/to/repo", []string{"web/modules/custom"}).Return(rector.ReturnOutput{}, assert.AnError)

		committer := &fakeCommitter{}
		updateRemoveDeprecations := NewDeprecationsRemover(logger, runner, composer)
		postCodeUpdate := workflow.NewPostCodeUpdateEvent(context.Background(), "/path/to/repo", committer)
		err := updateRemoveDeprecations.postCodeUpdateHandler(postCodeUpdate)

		require.Error(t, err)
		assert.Empty(t, committer.commits)
		composer.AssertExpectations(t)
		runner.AssertExpectations(t)
	})
}
