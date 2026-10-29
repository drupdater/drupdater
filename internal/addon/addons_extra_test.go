package addon

import (
	"context"
	"errors"
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
	t.Run("no token leaves the gitlab client unset", func(t *testing.T) {
		t.Setenv("DRUPALCODE_ACCESS_TOKEN", "")

		h := NewComposerPatches1(zap.NewNop(), NewMockComposer(t), NewMockDrupalOrg(t), nil)
		require.NotNil(t, h)
		assert.Nil(t, h.gitlab, "without a token there is no drupalcode client to look up issue forks with")
	})

	t.Run("a token configures the drupalcode gitlab client", func(t *testing.T) {
		t.Setenv("DRUPALCODE_ACCESS_TOKEN", "secret")

		h := NewComposerPatches1(zap.NewNop(), NewMockComposer(t), NewMockDrupalOrg(t), nil)
		require.NotNil(t, h)
		assert.NotNil(t, h.gitlab)
	})

	t.Run("drops patch files from the real filesystem", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "patches"), 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "patches", "a.patch"), []byte("x"), 0o600))

		h := NewComposerPatches1(zap.NewNop(), NewMockComposer(t), NewMockDrupalOrg(t), nil)
		require.NoError(t, h.dropPatchFile(dir, "patches/a.patch"))
		assert.NoFileExists(t, filepath.Join(dir, "patches", "a.patch"))
	})
}

func TestPatchRemovalFailuresAreLogged(t *testing.T) {
	// The patch is still reported as removed, so the log is the only trace of a file left behind.
	newHandler := func(t *testing.T, removeErr error) (*ComposerPatches1, *observer.ObservedLogs) {
		t.Helper()
		core, logs := observer.New(zapcore.ErrorLevel)
		composerService := NewMockComposer(t)
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

func TestComposerAllowPluginsNilMapIsNotAPanic(t *testing.T) {
	// A project with no per-package allow-plugins entries yields an empty map. If the addon
	// ever holds a nil map, assigning a newly discovered plugin into it panics.
	composerService := NewMockComposer(t)
	composerService.EXPECT().GetInstalledPlugins(anyCtx, "/tmp").Return(map[string]any{"new/plugin": nil}, nil)
	composerService.EXPECT().SetAllowPlugins(anyCtx, "/tmp", map[string]bool{"new/plugin": false}).Return(nil)

	ap := NewComposerAllowPlugins(zap.NewNop(), composerService)
	ap.allowPlugins = nil // as if GetAllowPlugins had produced nothing

	err := ap.postComposerUpdateHandler(workflow.NewPostComposerUpdateEvent(t.Context(), "/tmp", nil))
	require.NoError(t, err)
	assert.Equal(t, []string{"new/plugin"}, ap.newAllowPlugins)
}

func TestComposerAllowPluginsErrors(t *testing.T) {
	t.Run("reading the config fails", func(t *testing.T) {
		composerService := NewMockComposer(t)
		composerService.EXPECT().GetAllowPlugins(anyCtx, "/tmp").Return(nil, assert.AnError)

		ap := NewComposerAllowPlugins(zap.NewNop(), composerService)
		err := ap.preComposerUpdateHandler(workflow.NewPreComposerUpdateEvent(t.Context(), "/tmp", nil, nil, nil, false))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get composer allow plugins")
	})

	t.Run("listing installed plugins fails", func(t *testing.T) {
		composerService := NewMockComposer(t)
		composerService.EXPECT().GetInstalledPlugins(anyCtx, "/tmp").Return(nil, assert.AnError)

		ap := NewComposerAllowPlugins(zap.NewNop(), composerService)
		require.Error(t, ap.postComposerUpdateHandler(workflow.NewPostComposerUpdateEvent(t.Context(), "/tmp", nil)))
	})

	t.Run("writing the config back fails", func(t *testing.T) {
		composerService := NewMockComposer(t)
		composerService.EXPECT().GetInstalledPlugins(anyCtx, "/tmp").Return(map[string]any{}, nil)
		composerService.EXPECT().SetAllowPlugins(anyCtx, "/tmp", mock.Anything).Return(assert.AnError)

		ap := NewComposerAllowPlugins(zap.NewNop(), composerService)
		ap.allowPlugins = map[string]bool{}
		require.Error(t, ap.postComposerUpdateHandler(workflow.NewPostComposerUpdateEvent(t.Context(), "/tmp", nil)))
	})
}

func TestComposerDiffFailure(t *testing.T) {
	composerService := NewMockComposer(t)
	composerService.EXPECT().Diff(anyCtx, "/tmp").Return(nil, assert.AnError)

	cd := NewComposerDiff(zap.NewNop(), composerService)
	err := cd.postComposerUpdateHandler(workflow.NewPostComposerUpdateEvent(t.Context(), "/tmp", nil))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get diff")
}

func TestComposerNormalizerErrors(t *testing.T) {
	t.Run("install check fails", func(t *testing.T) {
		composerService := NewMockComposer(t)
		composerService.EXPECT().IsPackageInstalled(anyCtx, "/tmp", "ergebnis/composer-normalize").Return(false, assert.AnError)

		cn := NewComposerNormalizer(zap.NewNop(), composerService)
		require.Error(t, cn.postComposerUpdateHandler(workflow.NewPostComposerUpdateEvent(t.Context(), "/tmp", nil)))
	})

	t.Run("normalize fails", func(t *testing.T) {
		composerService := NewMockComposer(t)
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
		composerService := NewMockComposer(t)
		composerService.EXPECT().IsPackageInstalled(anyCtx, "/tmp", "palantirnet/drupal-rector").Return(true, nil)
		composerService.EXPECT().GetCustomCodeDirectories(anyCtx, "/tmp").Return(nil, assert.AnError)

		dr := NewDeprecationsRemover(zap.NewNop(), NewMockRector(t), NewMockRector(t), composerService)
		require.Error(t, dr.postCodeUpdateHandler(workflow.NewPostCodeUpdateEvent(t.Context(), "/tmp", nil)))
	})

	t.Run("committing the removals fails", func(t *testing.T) {
		composerService := NewMockComposer(t)
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
	newEvent := func(committer workflow.Committer) *workflow.PostSiteUpdateEvent {
		return workflow.NewPostSiteUpdateEvent(t.Context(), "/tmp", committer, "site1")
	}

	t.Run("module check fails", func(t *testing.T) {
		drushService := NewMockDrush(t)
		drushService.EXPECT().IsModuleEnabled(anyCtx, "/tmp", "site1", "locale_deploy").Return(false, assert.AnError)

		tu := NewTranslationsUpdater(zap.NewNop(), drushService)
		require.Error(t, tu.postSiteUpdateHandler(newEvent(&fakeCommitter{})))
	})

	t.Run("localizing translations fails", func(t *testing.T) {
		drushService := NewMockDrush(t)
		drushService.EXPECT().IsModuleEnabled(anyCtx, "/tmp", "site1", "locale_deploy").Return(true, nil)
		drushService.EXPECT().LocalizeTranslations(anyCtx, "/tmp", "site1").Return(assert.AnError)

		tu := NewTranslationsUpdater(zap.NewNop(), drushService)
		require.Error(t, tu.postSiteUpdateHandler(newEvent(&fakeCommitter{})))
	})

	t.Run("an unresolvable translation path is skipped, not fatal", func(t *testing.T) {
		// GetTranslationPath refuses to return an empty path, because committing that would
		// commit the entire working tree.
		drushService := NewMockDrush(t)
		drushService.EXPECT().IsModuleEnabled(anyCtx, "/tmp", "site1", "locale_deploy").Return(true, nil)
		drushService.EXPECT().LocalizeTranslations(anyCtx, "/tmp", "site1").Return(nil)
		drushService.EXPECT().GetTranslationPath(anyCtx, "/tmp", "site1", true).Return("", errors.New("does not resolve"))

		tu := NewTranslationsUpdater(zap.NewNop(), drushService)
		require.NoError(t, tu.postSiteUpdateHandler(newEvent(&fakeCommitter{})))
	})

	t.Run("committing the translations fails", func(t *testing.T) {
		drushService := NewMockDrush(t)
		drushService.EXPECT().IsModuleEnabled(anyCtx, "/tmp", "site1", "locale_deploy").Return(true, nil)
		drushService.EXPECT().LocalizeTranslations(anyCtx, "/tmp", "site1").Return(nil)
		drushService.EXPECT().GetTranslationPath(anyCtx, "/tmp", "site1", true).Return("translations", nil)

		tu := NewTranslationsUpdater(zap.NewNop(), drushService)
		err := tu.postSiteUpdateHandler(newEvent(&fakeCommitter{err: assert.AnError}))
		require.ErrorIs(t, err, assert.AnError)
		assert.Contains(t, err.Error(), "failed to commit translation path")
		assert.Empty(t, tu.results, "a site whose commit failed has no outcome to report")
	})
}

func TestUpdateHooksHandlerError(t *testing.T) {
	drushService := NewMockDrush(t)
	drushService.EXPECT().GetUpdateHooks(anyCtx, "/tmp", "site1").Return(nil, assert.AnError)

	uh := NewUpdateHooks(zap.NewNop(), drushService)
	err := uh.preSiteUpdateHandler(workflow.NewPreSiteUpdateEvent(t.Context(), "/tmp", nil, "site1"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get update hooks")
}

func TestUnsupportedModulesDeduplicatesAcrossSites(t *testing.T) {
	// Multisite runs check every site; the same end-of-life module must be reported once.
	module := drush.UnsupportedModule{Name: "old_module", InstalledVersion: "1.0", RecommendedVersion: "None"}

	drushService := NewMockDrush(t)
	drushService.EXPECT().GetUnsupportedModules(anyCtx, "/tmp", "site1").Return([]drush.UnsupportedModule{module}, nil)
	drushService.EXPECT().GetUnsupportedModules(anyCtx, "/tmp", "site2").Return([]drush.UnsupportedModule{module}, nil)

	um := NewUnsupportedModules(zap.NewNop(), drushService)
	require.NoError(t, um.preSiteUpdateHandler(workflow.NewPreSiteUpdateEvent(t.Context(), "/tmp", nil, "site1")))
	require.NoError(t, um.preSiteUpdateHandler(workflow.NewPreSiteUpdateEvent(t.Context(), "/tmp", nil, "site2")))

	assert.Equal(t, []drush.UnsupportedModule{module}, um.ReportData())
}

// anyCtx matches any non-nil context.Context in a mock expectation. mock.Anything would let a
// call site drop the context entirely, silently disabling cancellation and the run timeout.
var anyCtx = mock.MatchedBy(func(ctx context.Context) bool { return ctx != nil })
