package addon

import (
	"context"
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
	mockDrush := NewMockTranslationsDrush(t)
	handler := NewTranslationsUpdater(zap.NewNop(), mockDrush)
	committer := &fakeCommitter{}

	event := workflow.NewPostSiteUpdateEvent(context.Background(), "/tmp", committer, "example.com", drush.Facts{Modules: []string{"locale"}})
	require.NoError(t, handler.postSiteUpdateHandler(event))

	// Without it, "skipped because locale_deploy is off" and "changed nothing" read the same.
	assert.Equal(t, map[string]TranslationResult{
		"example.com": {Skipped: "locale_deploy not enabled"},
	}, handler.results.All())
	assert.Empty(t, committer.commits)
}

func TestUpdateTranslationsEventHandlerWithLocaleDeploy(t *testing.T) {
	project, facts := translationsSite(t)
	mockDrush := NewMockTranslationsDrush(t)
	handler := NewTranslationsUpdater(zap.NewNop(), mockDrush)
	committer := &fakeCommitter{}
	mockDrush.EXPECT().LocalizeTranslations(anyCtx, project, "example.com").Return(nil)

	event := workflow.NewPostSiteUpdateEvent(context.Background(), project, committer, "example.com", facts)
	require.NoError(t, handler.postSiteUpdateHandler(event))

	assert.Equal(t, map[string]TranslationResult{
		"example.com": {Path: "translations", Updated: true},
	}, handler.results.All(), "a committed translation update has to be reported as updated")
	assert.Equal(t, []recordedCommit{{message: "Update translations", paths: []string{"translations"}}}, committer.commits)
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
			committer := &fakeCommitter{}
			mockDrush.EXPECT().LocalizeTranslations(anyCtx, "/tmp", "example.com").Return(nil)

			facts := drush.Facts{Modules: []string{"locale_deploy"}, TranslationPath: translationPath}
			event := workflow.NewPostSiteUpdateEvent(context.Background(), "/tmp", committer, "example.com", facts)
			require.NoError(t, handler.postSiteUpdateHandler(event))

			result := handler.results.All()["example.com"]
			assert.Empty(t, result.Path)
			assert.False(t, result.Updated)
			assert.Contains(t, result.Skipped, "translation path not available")
			assert.Empty(t, committer.commits)
		})
	}
}

func TestUpdateTranslationsEventHandlerRecordsNothingToCommit(t *testing.T) {
	project, facts := translationsSite(t)
	mockDrush := NewMockTranslationsDrush(t)
	handler := NewTranslationsUpdater(zap.NewNop(), mockDrush)
	mockDrush.EXPECT().LocalizeTranslations(anyCtx, project, "example.com").Return(nil)

	event := workflow.NewPostSiteUpdateEvent(context.Background(), project, &fakeCommitter{unchanged: true}, "example.com", facts)
	require.NoError(t, handler.postSiteUpdateHandler(event))

	assert.Equal(t, map[string]TranslationResult{
		"example.com": {Path: "translations"},
	}, handler.results.All())
}
