package addon

import (
	"testing"

	"github.com/drupdater/drupdater/internal/composer"
	"github.com/drupdater/drupdater/internal/rector"
	"github.com/drupdater/drupdater/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// deprecationFixes sorts each file's rules, which in place would reorder data the caller still owns.
func TestDeprecationFixes_DoesNotSortRectorsOutputInPlace(t *testing.T) {
	output := rector.ReturnOutput{
		ChangedFiles: []string{"web/modules/custom/a.php"},
		FileDiffs: []rector.ReturnOutputFillDiff{
			{File: "web/modules/custom/a.php", AppliedRectors: []string{"ZRector", "ARector"}},
		},
	}

	fixes := deprecationFixes(output)

	assert.Equal(t, []string{"ZRector", "ARector"}, output.FileDiffs[0].AppliedRectors,
		"rector's own output was reordered")
	assert.Equal(t, []string{"ARector", "ZRector"}, fixes[0].AppliedRectors,
		"the report section is still sorted")
}

func TestRemoveDeprecations(t *testing.T) {
	logger := zap.NewNop()
	noChanges := rector.ReturnOutput{
		ChangedFiles: []string{},
		FileDiffs:    []rector.ReturnOutputFillDiff{},
	}

	t.Run("runs the project's own Rector when it has drupal-rector", func(t *testing.T) {
		composer := NewMockDeprecationsComposer(t)
		composer.EXPECT().Project(anyCtx, "/path/to/repo").Return(withCustomCode(lockedProject("palantirnet/drupal-rector"), "web/modules/custom"), nil)
		project := NewMockRector(t)
		project.EXPECT().Run(anyCtx, "/path/to/repo", []string{"web/modules/custom"}).Return(noChanges, nil)

		committer := &fakeCommitter{}
		dr := NewDeprecationsRemover(logger, project, NewMockRector(t), composer)
		_, err := firePostCodeUpdate(dr, workflow.NewPostCodeUpdateEvent(t.Context(), "/path/to/repo", committer))

		require.NoError(t, err)
		assert.Empty(t, committer.commits)
	})

	t.Run("runs the bundled Rector when the project has none, committing nothing to composer.*", func(t *testing.T) {
		composer := NewMockDeprecationsComposer(t)
		composer.EXPECT().Project(anyCtx, "/path/to/repo").Return(withCustomCode(lockedProject(), "web/modules/custom"), nil)
		bundled := NewMockRector(t)
		bundled.EXPECT().Run(anyCtx, "/path/to/repo", []string{"web/modules/custom"}).Return(rector.ReturnOutput{
			ChangedFiles: []string{"web/modules/custom/a.php"},
			Totals:       rector.ReturnOutputTotals{ChangedFiles: 1},
		}, nil)

		committer := &fakeCommitter{}
		dr := NewDeprecationsRemover(logger, NewMockRector(t), bundled, composer)
		_, err := firePostCodeUpdate(dr, workflow.NewPostCodeUpdateEvent(t.Context(), "/path/to/repo", committer))

		require.NoError(t, err)
		assert.Equal(t, []recordedCommit{
			{message: "Remove deprecations", paths: []string{"web/modules/custom/a.php"}},
		}, committer.commits)
	})

	t.Run("skips, with a warning, when the project's Rector lacks drupal-rector", func(t *testing.T) {
		core, logs := observer.New(zapcore.WarnLevel)
		composer := NewMockDeprecationsComposer(t)
		composer.EXPECT().Project(anyCtx, "/path/to/repo").Return(lockedProject("rector/rector"), nil)

		committer := &fakeCommitter{}
		dr := NewDeprecationsRemover(zap.New(core), NewMockRector(t), NewMockRector(t), composer)
		p, err := firePostCodeUpdate(dr, workflow.NewPostCodeUpdateEvent(t.Context(), "/path/to/repo", committer))

		require.NoError(t, err)
		assert.Empty(t, committer.commits)
		assert.Nil(t, reportSection(p, "deprecations_remover"))
		require.Equal(t, 1, logs.Len())
		assert.Contains(t, logs.All()[0].Message, "rector/rector but not palantirnet/drupal-rector")
	})

	t.Run("records and commits what Rector changed", func(t *testing.T) {
		composer := NewMockDeprecationsComposer(t)
		composer.EXPECT().Project(anyCtx, "/path/to/repo").Return(withCustomCode(lockedProject("palantirnet/drupal-rector"), "web/modules/custom"), nil)

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
		dr := NewDeprecationsRemover(logger, runner, NewMockRector(t), composer)
		p, err := firePostCodeUpdate(dr, workflow.NewPostCodeUpdateEvent(t.Context(), "/path/to/repo", committer))

		require.NoError(t, err)
		assert.Equal(t, []recordedCommit{
			{message: "Remove deprecations", paths: []string{"tests/Drupal/FunctionalJavascriptTests/ThunderOrgTestHomePageTest.php"}},
		}, committer.commits)
		assert.Equal(t, []DeprecationFix{{
			File: "tests/Drupal/FunctionalJavascriptTests/ThunderOrgTestHomePageTest.php",
			AppliedRectors: []string{
				"DrupalRector\\Drupal8\\Rector\\Deprecation\\FunctionalTestDefaultThemePropertyRector",
				"DrupalRector\\Drupal9\\Rector\\Property\\ProtectedStaticModulesPropertyRector",
			},
		}}, reportSection(p, "deprecations_remover"))
	})

	t.Run("a failing Rector fails the handler", func(t *testing.T) {
		composer := NewMockDeprecationsComposer(t)
		composer.EXPECT().Project(anyCtx, "/path/to/repo").Return(withCustomCode(lockedProject("palantirnet/drupal-rector"), "web/modules/custom"), nil)

		runner := NewMockRector(t)
		runner.EXPECT().Run(anyCtx, "/path/to/repo", []string{"web/modules/custom"}).Return(rector.ReturnOutput{}, assert.AnError)

		committer := &fakeCommitter{}
		dr := NewDeprecationsRemover(logger, runner, NewMockRector(t), composer)
		_, err := firePostCodeUpdate(dr, workflow.NewPostCodeUpdateEvent(t.Context(), "/path/to/repo", committer))

		require.ErrorIs(t, err, assert.AnError)
		assert.Empty(t, committer.commits)
	})
}

func TestDeprecationsRemoverErrors(t *testing.T) {
	t.Run("reading the project fails", func(t *testing.T) {
		composerService := NewMockDeprecationsComposer(t)
		composerService.EXPECT().Project(anyCtx, "/tmp").Return(composer.Project{}, assert.AnError)

		dr := NewDeprecationsRemover(zap.NewNop(), NewMockRector(t), NewMockRector(t), composerService)
		_, err := dr.postCodeUpdateHandler(workflow.NewPostCodeUpdateEvent(t.Context(), "/tmp", nil))
		require.ErrorIs(t, err, assert.AnError)
	})

	t.Run("a project without a web root has no custom code to find", func(t *testing.T) {
		composerService := NewMockDeprecationsComposer(t)
		composerService.EXPECT().Project(anyCtx, "/tmp").Return(composer.Project{}, nil)

		dr := NewDeprecationsRemover(zap.NewNop(), NewMockRector(t), NewMockRector(t), composerService)
		_, err := dr.postCodeUpdateHandler(workflow.NewPostCodeUpdateEvent(t.Context(), "/tmp", nil))
		require.Error(t, err)
	})

	t.Run("committing the removals fails", func(t *testing.T) {
		composerService := NewMockDeprecationsComposer(t)
		composerService.EXPECT().Project(anyCtx, "/tmp").Return(withCustomCode(lockedProject("palantirnet/drupal-rector"), "web/modules/custom"), nil)

		runner := NewMockRector(t)
		runner.EXPECT().Run(anyCtx, "/tmp", []string{"web/modules/custom"}).Return(rector.ReturnOutput{
			Totals:       rector.ReturnOutputTotals{ChangedFiles: 1},
			ChangedFiles: []string{"web/modules/custom/a.php"},
		}, nil)

		committer := &fakeCommitter{err: assert.AnError}
		dr := NewDeprecationsRemover(zap.NewNop(), runner, NewMockRector(t), composerService)
		p, err := firePostCodeUpdate(dr, workflow.NewPostCodeUpdateEvent(t.Context(), "/tmp", committer))
		require.ErrorIs(t, err, assert.AnError)
		assert.Nil(t, reportSection(p, "deprecations_remover"), "a run whose commit failed records nothing")
	})
}
