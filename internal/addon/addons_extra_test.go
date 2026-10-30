package addon

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/drupdater/drupdater/internal/workflow"
	"github.com/drupdater/drupdater/pkg/composer"
	"github.com/drupdater/drupdater/pkg/drush"
	"github.com/drupdater/drupdater/pkg/rector"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestNewComposerPatches1(t *testing.T) {
	t.Run("keeps the drupalcode client it is given", func(t *testing.T) {
		drupalCode := NewMockDrupalCode(t)
		h := NewComposerPatches1(zap.NewNop(), NewMockPatchesComposer(t), NewMockDrupalOrg(t), drupalCode, nil)
		assert.Same(t, drupalCode, h.drupalCode)
	})

	t.Run("drops patch files from the real filesystem", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "patches"), 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "patches", "a.patch"), []byte("x"), 0o600))

		h := NewComposerPatches1(zap.NewNop(), NewMockPatchesComposer(t), NewMockDrupalOrg(t), nil, nil)
		require.NoError(t, h.dropPatchFile(dir, "patches/a.patch"))
		assert.NoFileExists(t, filepath.Join(dir, "patches", "a.patch"))
	})
}

func TestPatchRemovalFailuresAreLogged(t *testing.T) {
	// The patch is still reported as removed, so the log is the only trace of a file left behind.
	newHandler := func(t *testing.T, removeErr error) (*ComposerPatches1, *observer.ObservedLogs) {
		t.Helper()
		core, logs := observer.New(zapcore.ErrorLevel)
		composerService := NewMockPatchesComposer(t)
		composerService.EXPECT().IsPackageInstalled(anyCtx, "/project", "drupal/gone").Return(false, nil).Maybe()
		return &ComposerPatches1{
			logger:     zap.New(core),
			composer:   composerService,
			removeFile: func(string) error { return removeErr },
		}, logs
	}
	patches := func() map[string]map[string]string {
		return map[string]map[string]string{"drupal/gone": {"fix": "patches/gone.patch"}}
	}
	removal := composer.PackageChange{Action: "Remove", Package: "drupal/gone"}

	for _, removeErr := range []error{nil, assert.AnError} {
		h, logs := newHandler(t, removeErr)
		assert.Len(t, h.removePackagePatches("/project", removal, patches()), 1)
		assert.Equal(t, removeErr != nil, logs.FilterMessage("failed to remove patch").Len() == 1, "removed package, err=%v", removeErr)

		h, logs = newHandler(t, removeErr)
		assert.Len(t, h.removeUninstalledPackagePatches(t.Context(), "/project", patches()), 1)
		assert.Equal(t, removeErr != nil, logs.FilterMessage("failed to remove patch").Len() == 1, "uninstalled package, err=%v", removeErr)
	}
}

func TestComposerDiffFailure(t *testing.T) {
	composerService := NewMockDiffComposer(t)
	composerService.EXPECT().Diff(anyCtx, "/tmp").Return(nil, assert.AnError)

	cd := NewComposerDiff(zap.NewNop(), composerService)
	err := cd.postComposerUpdateHandler(workflow.NewPostComposerUpdateEvent(t.Context(), "/tmp", nil))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get diff")
}

func TestComposerNormalizerErrors(t *testing.T) {
	t.Run("install check fails", func(t *testing.T) {
		composerService := NewMockNormalizerComposer(t)
		composerService.EXPECT().IsPackageInstalled(anyCtx, "/tmp", "ergebnis/composer-normalize").Return(false, assert.AnError)

		cn := NewComposerNormalizer(zap.NewNop(), composerService)
		require.Error(t, cn.postComposerUpdateHandler(workflow.NewPostComposerUpdateEvent(t.Context(), "/tmp", nil)))
	})

	t.Run("normalize fails", func(t *testing.T) {
		composerService := NewMockNormalizerComposer(t)
		composerService.EXPECT().IsPackageInstalled(anyCtx, "/tmp", "ergebnis/composer-normalize").Return(true, nil)
		composerService.EXPECT().Normalize(anyCtx, "/tmp").Return("", assert.AnError)

		cn := NewComposerNormalizer(zap.NewNop(), composerService)
		err := cn.postComposerUpdateHandler(workflow.NewPostComposerUpdateEvent(t.Context(), "/tmp", nil))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to normalize composer.json")
	})
}

func TestDeprecationsRemoverErrors(t *testing.T) {
	t.Run("listing custom code directories fails", func(t *testing.T) {
		composerService := NewMockDeprecationsComposer(t)
		composerService.EXPECT().IsPackageInstalled(anyCtx, "/tmp", "palantirnet/drupal-rector").Return(true, nil)
		composerService.EXPECT().GetCustomCodeDirectories(anyCtx, "/tmp").Return(nil, assert.AnError)

		dr := NewDeprecationsRemover(zap.NewNop(), NewMockRector(t), NewMockRector(t), composerService)
		require.Error(t, dr.postCodeUpdateHandler(workflow.NewPostCodeUpdateEvent(t.Context(), "/tmp", nil)))
	})

	t.Run("committing the removals fails", func(t *testing.T) {
		composerService := NewMockDeprecationsComposer(t)
		composerService.EXPECT().IsPackageInstalled(anyCtx, "/tmp", "palantirnet/drupal-rector").Return(true, nil)
		composerService.EXPECT().GetCustomCodeDirectories(anyCtx, "/tmp").Return([]string{"web/modules/custom"}, nil)

		runner := NewMockRector(t)
		runner.EXPECT().Run(anyCtx, "/tmp", []string{"web/modules/custom"}).Return(rector.ReturnOutput{
			Totals:       rector.ReturnOutputTotals{ChangedFiles: 1},
			ChangedFiles: []string{"web/modules/custom/a.php"},
		}, nil)

		committer := &fakeCommitter{err: assert.AnError}
		dr := NewDeprecationsRemover(zap.NewNop(), runner, NewMockRector(t), composerService)
		require.ErrorIs(t, dr.postCodeUpdateHandler(workflow.NewPostCodeUpdateEvent(t.Context(), "/tmp", committer)), assert.AnError)
	})
}

func TestTranslationsUpdaterErrors(t *testing.T) {
	t.Run("localizing translations fails", func(t *testing.T) {
		drushService := NewMockTranslationsDrush(t)
		drushService.EXPECT().LocalizeTranslations(anyCtx, "/tmp", "site1").Return(assert.AnError)

		tu := NewTranslationsUpdater(zap.NewNop(), drushService)
		evt := workflow.NewPostSiteUpdateEvent(t.Context(), "/tmp", &fakeCommitter{}, "site1", drush.Facts{Modules: []string{"locale_deploy"}})
		require.ErrorIs(t, tu.postSiteUpdateHandler(evt), assert.AnError)
	})

	t.Run("committing the translations fails", func(t *testing.T) {
		project, facts := translationsSite(t)
		drushService := NewMockTranslationsDrush(t)
		drushService.EXPECT().LocalizeTranslations(anyCtx, project, "site1").Return(nil)

		tu := NewTranslationsUpdater(zap.NewNop(), drushService)
		err := tu.postSiteUpdateHandler(workflow.NewPostSiteUpdateEvent(t.Context(), project, &fakeCommitter{err: assert.AnError}, "site1", facts))
		require.ErrorIs(t, err, assert.AnError)
		assert.Contains(t, err.Error(), "failed to commit translation path")
		assert.Empty(t, tu.results.All(), "a site whose commit failed has no outcome to report")
	})
}

// anyCtx matches any non-nil context.Context in a mock expectation. mock.Anything would let a
// call site drop the context entirely, silently disabling cancellation and the run timeout.
var anyCtx = mock.MatchedBy(func(ctx context.Context) bool { return ctx != nil })
