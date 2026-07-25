package addon

import (
	"errors"
	"testing"

	"github.com/drupdater/drupdater/internal/services"
	"github.com/drupdater/drupdater/pkg/drush"
	"github.com/drupdater/drupdater/pkg/rector"
	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
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
}

func TestComposerAllowPluginsNilMapIsNotAPanic(t *testing.T) {
	// A project with no per-package allow-plugins entries yields an empty map. If the addon
	// ever holds a nil map, assigning a newly discovered plugin into it panics.
	composerService := NewMockComposer(t)
	composerService.EXPECT().GetInstalledPlugins(mock.Anything, "/tmp").Return(map[string]any{"new/plugin": nil}, nil)
	composerService.EXPECT().SetAllowPlugins(mock.Anything, "/tmp", map[string]bool{"new/plugin": false}).Return(nil)

	ap := NewComposerAllowPlugins(zap.NewNop(), composerService)
	ap.allowPlugins = nil // as if GetAllowPlugins had produced nothing

	err := ap.postComposerUpdateHandler(services.NewPostComposerUpdateEvent(t.Context(), "/tmp", nil))
	require.NoError(t, err)
	assert.Equal(t, []string{"new/plugin"}, ap.newAllowPlugins)
}

func TestComposerAllowPluginsErrors(t *testing.T) {
	t.Run("reading the config fails", func(t *testing.T) {
		composerService := NewMockComposer(t)
		composerService.EXPECT().GetAllowPlugins(mock.Anything, "/tmp").Return(nil, assert.AnError)

		ap := NewComposerAllowPlugins(zap.NewNop(), composerService)
		err := ap.preComposerUpdateHandler(services.NewPreComposerUpdateEvent(t.Context(), "/tmp", nil, nil, nil, false))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get composer allow plugins")
	})

	t.Run("listing installed plugins fails", func(t *testing.T) {
		composerService := NewMockComposer(t)
		composerService.EXPECT().GetInstalledPlugins(mock.Anything, "/tmp").Return(nil, assert.AnError)

		ap := NewComposerAllowPlugins(zap.NewNop(), composerService)
		require.Error(t, ap.postComposerUpdateHandler(services.NewPostComposerUpdateEvent(t.Context(), "/tmp", nil)))
	})

	t.Run("writing the config back fails", func(t *testing.T) {
		composerService := NewMockComposer(t)
		composerService.EXPECT().GetInstalledPlugins(mock.Anything, "/tmp").Return(map[string]any{}, nil)
		composerService.EXPECT().SetAllowPlugins(mock.Anything, "/tmp", mock.Anything).Return(assert.AnError)

		ap := NewComposerAllowPlugins(zap.NewNop(), composerService)
		ap.allowPlugins = map[string]bool{}
		require.Error(t, ap.postComposerUpdateHandler(services.NewPostComposerUpdateEvent(t.Context(), "/tmp", nil)))
	})
}

func TestComposerDiffLogFailureDoesNotFailTheRun(t *testing.T) {
	// The link-free table only feeds the run log. Failing to produce it must be reported but
	// must not abort an otherwise successful update.
	core, logs := observer.New(zap.WarnLevel)

	composerService := NewMockComposer(t)
	composerService.EXPECT().Diff(mock.Anything, "/tmp", true).Return("| linked table |", nil)
	composerService.EXPECT().Diff(mock.Anything, "/tmp", false).Return("", assert.AnError)

	cd := NewComposerDiff(zap.New(core), composerService)
	err := cd.postComposerUpdateHandler(services.NewPostComposerUpdateEvent(t.Context(), "/tmp", nil))

	require.NoError(t, err)
	assert.Equal(t, "| linked table |", cd.table, "the merge request still gets the linked table")
	require.Equal(t, 1, logs.Len())
	assert.Equal(t, "failed to render the dependency diff for the log", logs.All()[0].Message)
}

func TestComposerDiffFailure(t *testing.T) {
	composerService := NewMockComposer(t)
	composerService.EXPECT().Diff(mock.Anything, "/tmp", true).Return("", assert.AnError)

	cd := NewComposerDiff(zap.NewNop(), composerService)
	err := cd.postComposerUpdateHandler(services.NewPostComposerUpdateEvent(t.Context(), "/tmp", nil))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get diff")
}

func TestComposerNormalizerErrors(t *testing.T) {
	t.Run("install check fails", func(t *testing.T) {
		composerService := NewMockComposer(t)
		composerService.EXPECT().IsPackageInstalled(mock.Anything, "/tmp", "ergebnis/composer-normalize").Return(false, assert.AnError)

		cn := NewComposerNormalizer(zap.NewNop(), composerService)
		require.Error(t, cn.postComposerUpdateHandler(services.NewPostComposerUpdateEvent(t.Context(), "/tmp", nil)))
	})

	t.Run("normalize fails", func(t *testing.T) {
		composerService := NewMockComposer(t)
		composerService.EXPECT().IsPackageInstalled(mock.Anything, "/tmp", "ergebnis/composer-normalize").Return(true, nil)
		composerService.EXPECT().Normalize(mock.Anything, "/tmp").Return("", assert.AnError)

		cn := NewComposerNormalizer(zap.NewNop(), composerService)
		err := cn.postComposerUpdateHandler(services.NewPostComposerUpdateEvent(t.Context(), "/tmp", nil))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to normalize composer.json")
	})
}

func TestDeprecationsRemoverErrors(t *testing.T) {
	t.Run("installing rector fails", func(t *testing.T) {
		composerService := NewMockComposer(t)
		composerService.EXPECT().IsPackageInstalled(mock.Anything, "/tmp", "palantirnet/drupal-rector").Return(false, nil)
		composerService.EXPECT().Require(mock.Anything, "/tmp", []string{"palantirnet/drupal-rector"}).Return("", assert.AnError)

		dr := NewDeprecationsRemover(zap.NewNop(), NewMockRector(t), composerService)
		require.Error(t, dr.postCodeUpdateHandler(services.NewPostCodeUpdateEvent(t.Context(), "/tmp", nil)))
	})

	t.Run("listing custom code directories fails", func(t *testing.T) {
		composerService := NewMockComposer(t)
		composerService.EXPECT().IsPackageInstalled(mock.Anything, "/tmp", "palantirnet/drupal-rector").Return(true, nil)
		composerService.EXPECT().GetCustomCodeDirectories(mock.Anything, "/tmp").Return(nil, assert.AnError)

		dr := NewDeprecationsRemover(zap.NewNop(), NewMockRector(t), composerService)
		require.Error(t, dr.postCodeUpdateHandler(services.NewPostCodeUpdateEvent(t.Context(), "/tmp", nil)))
	})

	t.Run("removing the temporarily installed rector fails", func(t *testing.T) {
		composerService := NewMockComposer(t)
		composerService.EXPECT().IsPackageInstalled(mock.Anything, "/tmp", "palantirnet/drupal-rector").Return(false, nil)
		composerService.EXPECT().Require(mock.Anything, "/tmp", []string{"palantirnet/drupal-rector"}).Return("", nil)
		composerService.EXPECT().GetCustomCodeDirectories(mock.Anything, "/tmp").Return([]string{"web/modules/custom"}, nil)
		composerService.EXPECT().Remove(mock.Anything, "/tmp", []string{"palantirnet/drupal-rector"}).Return("", assert.AnError)

		runner := NewMockRector(t)
		runner.EXPECT().Run(mock.Anything, "/tmp", []string{"web/modules/custom"}).Return(rector.ReturnOutput{}, nil)

		dr := NewDeprecationsRemover(zap.NewNop(), runner, composerService)
		require.Error(t, dr.postCodeUpdateHandler(services.NewPostCodeUpdateEvent(t.Context(), "/tmp", nil)))
	})

	t.Run("staging a changed file fails", func(t *testing.T) {
		composerService := NewMockComposer(t)
		composerService.EXPECT().IsPackageInstalled(mock.Anything, "/tmp", "palantirnet/drupal-rector").Return(true, nil)
		composerService.EXPECT().GetCustomCodeDirectories(mock.Anything, "/tmp").Return([]string{"web/modules/custom"}, nil)

		runner := NewMockRector(t)
		runner.EXPECT().Run(mock.Anything, "/tmp", []string{"web/modules/custom"}).Return(rector.ReturnOutput{
			Totals:       rector.ReturnOutputTotals{ChangedFiles: 1},
			ChangedFiles: []string{"web/modules/custom/a.php"},
		}, nil)

		worktree := NewMockWorktree(t)
		worktree.EXPECT().Add("web/modules/custom/a.php").Return(plumbing.NewHash(""), assert.AnError)

		dr := NewDeprecationsRemover(zap.NewNop(), runner, composerService)
		require.Error(t, dr.postCodeUpdateHandler(services.NewPostCodeUpdateEvent(t.Context(), "/tmp", worktree)))
	})
}

func TestTranslationsUpdaterErrors(t *testing.T) {
	newEvent := func(worktree Worktree) *services.PostSiteUpdateEvent {
		return services.NewPostSiteUpdateEvent(t.Context(), "/tmp", worktree, "site1")
	}

	t.Run("module check fails", func(t *testing.T) {
		drushService := NewMockDrush(t)
		drushService.EXPECT().IsModuleEnabled(mock.Anything, "/tmp", "site1", "locale_deploy").Return(false, assert.AnError)

		tu := NewTranslationsUpdater(zap.NewNop(), drushService, NewMockRepository(t))
		require.Error(t, tu.postSiteUpdateHandler(newEvent(nil)))
	})

	t.Run("localizing translations fails", func(t *testing.T) {
		drushService := NewMockDrush(t)
		drushService.EXPECT().IsModuleEnabled(mock.Anything, "/tmp", "site1", "locale_deploy").Return(true, nil)
		drushService.EXPECT().LocalizeTranslations(mock.Anything, "/tmp", "site1").Return(assert.AnError)

		tu := NewTranslationsUpdater(zap.NewNop(), drushService, NewMockRepository(t))
		require.Error(t, tu.postSiteUpdateHandler(newEvent(nil)))
	})

	t.Run("an unresolvable translation path is skipped, not fatal", func(t *testing.T) {
		// GetTranslationPath refuses to return an empty path, because handing that to
		// Worktree.Add would stage the entire working tree.
		drushService := NewMockDrush(t)
		drushService.EXPECT().IsModuleEnabled(mock.Anything, "/tmp", "site1", "locale_deploy").Return(true, nil)
		drushService.EXPECT().LocalizeTranslations(mock.Anything, "/tmp", "site1").Return(nil)
		drushService.EXPECT().GetTranslationPath(mock.Anything, "/tmp", "site1", true).Return("", errors.New("does not resolve"))

		tu := NewTranslationsUpdater(zap.NewNop(), drushService, NewMockRepository(t))
		require.NoError(t, tu.postSiteUpdateHandler(newEvent(nil)))
	})

	t.Run("staging the translation path fails", func(t *testing.T) {
		drushService := NewMockDrush(t)
		drushService.EXPECT().IsModuleEnabled(mock.Anything, "/tmp", "site1", "locale_deploy").Return(true, nil)
		drushService.EXPECT().LocalizeTranslations(mock.Anything, "/tmp", "site1").Return(nil)
		drushService.EXPECT().GetTranslationPath(mock.Anything, "/tmp", "site1", true).Return("translations", nil)

		worktree := NewMockWorktree(t)
		worktree.EXPECT().Add("translations").Return(plumbing.NewHash(""), assert.AnError)

		tu := NewTranslationsUpdater(zap.NewNop(), drushService, NewMockRepository(t))
		err := tu.postSiteUpdateHandler(newEvent(worktree))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to add translation path")
	})

	t.Run("committing the translations fails", func(t *testing.T) {
		drushService := NewMockDrush(t)
		drushService.EXPECT().IsModuleEnabled(mock.Anything, "/tmp", "site1", "locale_deploy").Return(true, nil)
		drushService.EXPECT().LocalizeTranslations(mock.Anything, "/tmp", "site1").Return(nil)
		drushService.EXPECT().GetTranslationPath(mock.Anything, "/tmp", "site1", true).Return("translations", nil)

		worktree := NewMockWorktree(t)
		worktree.EXPECT().Add("translations").Return(plumbing.NewHash(""), nil)
		worktree.EXPECT().Status().Return(git.Status{}, nil)
		worktree.EXPECT().Commit("Update translations", &git.CommitOptions{}).Return(plumbing.NewHash(""), assert.AnError)

		repository := NewMockRepository(t)
		repository.EXPECT().IsSomethingStagedInPath(worktree, "translations").Return(true)

		tu := NewTranslationsUpdater(zap.NewNop(), drushService, repository)
		err := tu.postSiteUpdateHandler(newEvent(worktree))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to commit translation path")
	})
}

func TestUpdateHooksHandlerError(t *testing.T) {
	drushService := NewMockDrush(t)
	drushService.EXPECT().GetUpdateHooks(mock.Anything, "/tmp", "site1").Return(nil, assert.AnError)

	uh := NewUpdateHooks(zap.NewNop(), drushService)
	err := uh.preSiteUpdateHandler(services.NewPreSiteUpdateEvent(t.Context(), "/tmp", nil, "site1"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get update hooks")
}

func TestUnsupportedModulesDeduplicatesAcrossSites(t *testing.T) {
	// Multisite runs check every site; the same end-of-life module must be reported once.
	module := drush.UnsupportedModule{Name: "old_module", InstalledVersion: "1.0", RecommendedVersion: "None"}

	drushService := NewMockDrush(t)
	drushService.EXPECT().GetUnsupportedModules(mock.Anything, "/tmp", "site1").Return([]drush.UnsupportedModule{module}, nil)
	drushService.EXPECT().GetUnsupportedModules(mock.Anything, "/tmp", "site2").Return([]drush.UnsupportedModule{module}, nil)

	um := NewUnsupportedModules(zap.NewNop(), drushService)
	require.NoError(t, um.preSiteUpdateHandler(services.NewPreSiteUpdateEvent(t.Context(), "/tmp", nil, "site1")))
	require.NoError(t, um.preSiteUpdateHandler(services.NewPreSiteUpdateEvent(t.Context(), "/tmp", nil, "site2")))

	assert.Len(t, um.modules, 1)

	out, err := um.RenderTemplate()
	require.NoError(t, err)
	assert.Equal(t, 1, countOccurrences(out, "old_module"))
}

func countOccurrences(haystack string, needle string) int {
	count := 0
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			count++
		}
	}
	return count
}
