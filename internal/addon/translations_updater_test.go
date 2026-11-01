package addon

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/drupdater/drupdater/internal/drush"
	"github.com/drupdater/drupdater/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// translationsSite is a project with a translations directory, and a locale_deploy site's facts.
func translationsSite(t *testing.T) (string, drush.Facts) {
	t.Helper()
	project := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(project, "translations"), 0o755))
	return project, drush.Facts{
		Modules:         []string{"locale_deploy"},
		TranslationPath: filepath.Join(project, "translations"),
	}
}

func TestUpdateTranslationsEventHandlerWithoutLocaleDeploy(t *testing.T) {
	handler := NewTranslationsUpdater(zap.NewNop(), NewMockTranslationsDrush(t))

	event := workflow.NewPostSiteUpdateEvent(t.Context(), "/tmp", &fakeCommitter{}, "example.com", drush.Facts{Modules: []string{"locale"}})
	result, err := handler.postSiteUpdateHandler(event)
	require.NoError(t, err)

	// Without it, "skipped because locale_deploy is off" and "changed nothing" read the same.
	assert.Equal(t, workflow.Result[TranslationResult]{Finding: TranslationResult{Skipped: "locale_deploy not enabled"}}, result)
}

func TestUpdateTranslationsEventHandlerWithLocaleDeploy(t *testing.T) {
	project, facts := translationsSite(t)
	mockDrush := NewMockTranslationsDrush(t)
	handler := NewTranslationsUpdater(zap.NewNop(), mockDrush)
	mockDrush.EXPECT().LocalizeTranslations(anyCtx, project, "example.com").Return(nil)

	event := workflow.NewPostSiteUpdateEvent(t.Context(), project, &fakeCommitter{}, "example.com", facts)
	result, err := handler.postSiteUpdateHandler(event)
	require.NoError(t, err)

	assert.Equal(t, workflow.Result[TranslationResult]{
		Commits: []workflow.Commit{{Message: "Update translations", Paths: []string{"translations"}}},
		Finding: TranslationResult{Path: "translations"},
	}, result)
}

// A soft skip, never a commit: an empty path would commit the entire working tree.
func TestUpdateTranslationsEventHandlerSkipsWhenTranslationPathUnavailable(t *testing.T) {
	for name, translationPath := range map[string]string{
		"not configured":  "",
		"never created":   "/does/not/exist",
		"not a directory": "/dev/null",
	} {
		t.Run(name, func(t *testing.T) {
			mockDrush := NewMockTranslationsDrush(t)
			handler := NewTranslationsUpdater(zap.NewNop(), mockDrush)
			mockDrush.EXPECT().LocalizeTranslations(anyCtx, "/tmp", "example.com").Return(nil)

			facts := drush.Facts{Modules: []string{"locale_deploy"}, TranslationPath: translationPath}
			event := workflow.NewPostSiteUpdateEvent(t.Context(), "/tmp", &fakeCommitter{}, "example.com", facts)
			result, err := handler.postSiteUpdateHandler(event)
			require.NoError(t, err)

			assert.Empty(t, result.Commits)
			assert.Empty(t, result.Finding.Path)
			assert.Contains(t, result.Finding.Skipped, "translation path not available")
		})
	}
}

func TestTranslationsUpdaterReportsUpdatedOnlyWhenTheCommitWasMade(t *testing.T) {
	for name, tt := range map[string]struct {
		committer *fakeCommitter
		want      TranslationResult
	}{
		"committed":         {&fakeCommitter{}, TranslationResult{Path: "translations", Updated: true}},
		"nothing to commit": {&fakeCommitter{unchanged: true}, TranslationResult{Path: "translations"}},
	} {
		t.Run(name, func(t *testing.T) {
			project, facts := translationsSite(t)
			mockDrush := NewMockTranslationsDrush(t)
			mockDrush.EXPECT().LocalizeTranslations(anyCtx, project, "example.com").Return(nil)
			p := workflow.NewPipeline([]workflow.Addon{NewTranslationsUpdater(zap.NewNop(), mockDrush)})

			require.NoError(t, workflow.Fire(p, workflow.NewPostSiteUpdateEvent(t.Context(), project, tt.committer, "example.com", facts)))

			assert.Equal(t, map[string]TranslationResult{"example.com": tt.want}, reportSection(p, "translations_updater"))
			assert.Equal(t, []recordedCommit{{message: "Update translations", paths: []string{"translations"}}}, tt.committer.commits)
		})
	}
}

func TestTranslationsUpdaterErrors(t *testing.T) {
	t.Run("localizing translations fails", func(t *testing.T) {
		drushService := NewMockTranslationsDrush(t)
		drushService.EXPECT().LocalizeTranslations(anyCtx, "/tmp", "site1").Return(assert.AnError)

		tu := NewTranslationsUpdater(zap.NewNop(), drushService)
		evt := workflow.NewPostSiteUpdateEvent(t.Context(), "/tmp", &fakeCommitter{}, "site1", drush.Facts{Modules: []string{"locale_deploy"}})
		_, err := tu.postSiteUpdateHandler(evt)
		require.ErrorIs(t, err, assert.AnError)
	})

	t.Run("committing the translations fails", func(t *testing.T) {
		project, facts := translationsSite(t)
		drushService := NewMockTranslationsDrush(t)
		drushService.EXPECT().LocalizeTranslations(anyCtx, project, "site1").Return(nil)

		p := workflow.NewPipeline([]workflow.Addon{NewTranslationsUpdater(zap.NewNop(), drushService)})
		err := workflow.Fire(p, workflow.NewPostSiteUpdateEvent(t.Context(), project, &fakeCommitter{err: assert.AnError}, "site1", facts))
		require.ErrorIs(t, err, assert.AnError)
		assert.Nil(t, reportSection(p, "translations_updater"), "a site whose commit failed has no outcome to report")
	})
}
