package drupal

import (
	"os"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
	"gopkg.in/yaml.v3"
)

const coreExtensionWithSqlite = `
module:
  node: 0
  sqlite: 0
theme:
  stark: 0
profile: standard
`

const coreExtensionWithoutSqlite = `
module:
  node: 0
theme:
  stark: 0
profile: standard
`

// newTestInstaller wires an Installer onto an in-memory filesystem with the given
// core.extension.yml and an (optionally pre-existing) settings.php.
func newTestInstaller(t *testing.T, coreExtension string, settings string) (*Installer, afero.Fs, *MockDrush, *MockComposer) {
	t.Helper()
	installer, fs, drush, composer, _ := newTestInstallerWithLogs(t, coreExtension, settings)
	return installer, fs, drush, composer
}

// newTestInstallerWithLogs captures the debug output, which is the only external trace of
// several of the installer's decisions.
func newTestInstallerWithLogs(t *testing.T, coreExtension string, settings string) (*Installer, afero.Fs, *MockDrush, *MockComposer, *observer.ObservedLogs) {
	t.Helper()

	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/config/sync/core.extension.yml", []byte(coreExtension), 0o644))
	require.NoError(t, afero.WriteFile(fs, "/project/web/sites/site1/settings.php", []byte(settings), 0o644))

	drush := NewMockDrush(t)
	composer := NewMockComposer(t)
	core, logs := observer.New(zapcore.DebugLevel)

	return &Installer{
		logger:   zap.New(core),
		drush:    drush,
		composer: composer,
		fs:       fs,
	}, fs, drush, composer, logs
}

func TestNewInstaller(t *testing.T) {
	logger := zap.NewNop()
	drush := NewMockDrush(t)
	composer := NewMockComposer(t)

	installer := NewInstaller(logger, drush, composer)

	require.NotNil(t, installer)
	assert.Equal(t, logger, installer.logger)
	assert.Equal(t, drush, installer.drush)
	assert.Equal(t, composer, installer.composer)
	assert.NotNil(t, installer.fs)
}

func TestInstallStopsAtTheFirstFailure(t *testing.T) {
	// Each guard must stop the run: continuing past a failed database configuration installs
	// the site against the project's real database.
	t.Run("database configuration fails", func(t *testing.T) {
		installer, _, _, composer := newTestInstaller(t, coreExtensionWithSqlite, "<?php\n")
		composer.EXPECT().GetConfig(t.Context(), "/project", "extra.drupal-scaffold.locations.web-root").
			Return("", assert.AnError)

		// No drush expectations: reaching RemoveProfile or InstallSite would fail the mock.
		err := installer.Install(t.Context(), "/project", "site1")
		require.Error(t, err)
		require.ErrorIs(t, err, assert.AnError)
	})

	t.Run("profile removal fails", func(t *testing.T) {
		installer, _, drush, composer := newTestInstaller(t, coreExtensionWithSqlite, "<?php\n")
		composer.EXPECT().GetConfig(t.Context(), "/project", "extra.drupal-scaffold.locations.web-root").Return("web", nil)
		// First lookup is ConfigureDatabase's sqlite check, second is RemoveProfile's.
		drush.EXPECT().GetConfigSyncDir(t.Context(), "/project", "site1", false).Return("/config/sync", nil).Once()
		drush.EXPECT().GetConfigSyncDir(t.Context(), "/project", "site1", false).Return("", assert.AnError).Once()

		err := installer.Install(t.Context(), "/project", "site1")
		require.Error(t, err)
		require.ErrorIs(t, err, assert.AnError)
	})
}

func TestConfigureDatabaseIsIdempotent(t *testing.T) {
	// Each site is configured twice against the same settings.php, and the second call must
	// not append a second copy of the settings.
	installer, fs, drush, composer, logs := newTestInstallerWithLogs(t, coreExtensionWithSqlite, "<?php\n")

	composer.EXPECT().GetConfig(t.Context(), "/project", "extra.drupal-scaffold.locations.web-root").Return("web", nil)
	drush.EXPECT().GetConfigSyncDir(t.Context(), "/project", "site1", false).Return("/config/sync", nil)

	require.NoError(t, installer.ConfigureDatabase(t.Context(), "/project", "site1"))

	after, err := afero.ReadFile(fs, "/project/web/sites/site1/settings.php")
	require.NoError(t, err)
	first := string(after)
	assert.Equal(t, 1, strings.Count(first, settingsMarker))
	assert.Equal(t, 1, strings.Count(first, "$settings['hash_salt']"))

	// Second call: the marker is present, so the append is skipped without touching the file.
	// core.extension.yml is still checked, which is why GetConfigSyncDir is reached again.
	require.NoError(t, installer.ConfigureDatabase(t.Context(), "/project", "site1"))

	after, err = afero.ReadFile(fs, "/project/web/sites/site1/settings.php")
	require.NoError(t, err)
	assert.Equal(t, first, string(after), "settings.php must be unchanged by the second call")

	// The skip is only visible in the log, so without this a silent second write and a correct
	// skip would look identical from the outside.
	assert.Equal(t, 1, logs.FilterMessage("settings already configured, skipping").Len())
	assert.Equal(t, 1, logs.FilterMessage("writing settings").Len())
	writing := logs.FilterMessage("writing settings").All()[0].ContextMap()
	assert.Equal(t, "/project/web/sites/site1/settings.php", writing["path"])
}

func TestConfigureDatabaseEnablesSqliteWhenMissing(t *testing.T) {
	installer, fs, drush, composer := newTestInstaller(t, coreExtensionWithoutSqlite, "<?php\n")

	composer.EXPECT().GetConfig(t.Context(), "/project", "extra.drupal-scaffold.locations.web-root").Return("web/", nil)
	drush.EXPECT().GetConfigSyncDir(t.Context(), "/project", "site1", false).Return("/config/sync", nil)

	require.NoError(t, installer.ConfigureDatabase(t.Context(), "/project", "site1"))

	// sqlite is added to core.extension.yml...
	core, err := afero.ReadFile(fs, "/config/sync/core.extension.yml")
	require.NoError(t, err)
	assert.Contains(t, string(core), "sqlite: 0")

	// ...and excluded from config export so it does not leak into the merge request.
	settings, err := afero.ReadFile(fs, "/project/web/sites/site1/settings.php")
	require.NoError(t, err)
	assert.Contains(t, string(settings), "config_exclude_modules")
}

func TestConfigureDatabaseErrors(t *testing.T) {
	t.Run("web root lookup fails", func(t *testing.T) {
		installer, _, _, composer := newTestInstaller(t, coreExtensionWithSqlite, "<?php\n")
		composer.EXPECT().GetConfig(t.Context(), "/project", "extra.drupal-scaffold.locations.web-root").
			Return("", assert.AnError)

		err := installer.ConfigureDatabase(t.Context(), "/project", "site1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get Drupal web dir")
		require.ErrorIs(t, err, assert.AnError, "the cause must survive the wrapper")
	})

	t.Run("enabling sqlite fails", func(t *testing.T) {
		installer, _, drush, composer := newTestInstaller(t, coreExtensionWithoutSqlite, "<?php\n")
		composer.EXPECT().GetConfig(t.Context(), "/project", "extra.drupal-scaffold.locations.web-root").Return("web", nil)
		// The first lookup (isSqliteModuleEnabled) succeeds, addSqliteModule then fails.
		drush.EXPECT().GetConfigSyncDir(t.Context(), "/project", "site1", false).Return("/config/sync", nil).Once()
		drush.EXPECT().GetConfigSyncDir(t.Context(), "/project", "site1", false).Return("", assert.AnError).Once()

		err := installer.ConfigureDatabase(t.Context(), "/project", "site1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to enable sqlite module")
		require.ErrorIs(t, err, assert.AnError)
	})

	t.Run("settings.php does not exist", func(t *testing.T) {
		// The snippet is an append to an existing settings.php; on its own it is not a valid
		// settings file, so a missing one is an error rather than something to create.
		installer, fs, drush, composer := newTestInstaller(t, coreExtensionWithSqlite, "<?php\n")
		require.NoError(t, fs.Remove("/project/web/sites/site1/settings.php"))

		composer.EXPECT().GetConfig(t.Context(), "/project", "extra.drupal-scaffold.locations.web-root").Return("web", nil)
		drush.EXPECT().GetConfigSyncDir(t.Context(), "/project", "site1", false).Return("/config/sync", nil)

		err := installer.ConfigureDatabase(t.Context(), "/project", "site1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to open settings file")
	})
}

func TestIsSqliteModuleEnabled(t *testing.T) {
	t.Run("reports enabled when listed with weight 0", func(t *testing.T) {
		installer, _, drush, _ := newTestInstaller(t, coreExtensionWithSqlite, "")
		drush.EXPECT().GetConfigSyncDir(t.Context(), "/project", "site1", false).Return("/config/sync", nil)

		enabled, err := installer.isSqliteModuleEnabled(t.Context(), "/project", "site1")
		require.NoError(t, err)
		assert.True(t, enabled)
	})

	t.Run("reports not enabled when absent", func(t *testing.T) {
		installer, _, drush, _ := newTestInstaller(t, coreExtensionWithoutSqlite, "")
		drush.EXPECT().GetConfigSyncDir(t.Context(), "/project", "site1", false).Return("/config/sync", nil)

		enabled, err := installer.isSqliteModuleEnabled(t.Context(), "/project", "site1")
		require.NoError(t, err)
		assert.False(t, enabled)
	})

	t.Run("reports not enabled when listed with a non-zero weight", func(t *testing.T) {
		// Both halves of the check matter: the key being present is not enough, its weight has
		// to be 0. Without this case the "exists" and "weight is 0" clauses are interchangeable.
		installer, _, drush, _ := newTestInstaller(t, "module:\n  node: 0\n  sqlite: 10\n", "")
		drush.EXPECT().GetConfigSyncDir(t.Context(), "/project", "site1", false).Return("/config/sync", nil)

		enabled, err := installer.isSqliteModuleEnabled(t.Context(), "/project", "site1")
		require.NoError(t, err)
		assert.False(t, enabled)
	})

	t.Run("errors when the config sync dir cannot be resolved", func(t *testing.T) {
		installer, _, drush, _ := newTestInstaller(t, coreExtensionWithSqlite, "")
		drush.EXPECT().GetConfigSyncDir(t.Context(), "/project", "site1", false).Return("", assert.AnError)

		_, err := installer.isSqliteModuleEnabled(t.Context(), "/project", "site1")
		require.Error(t, err)
		require.ErrorIs(t, err, assert.AnError, "the lookup failure must be returned as-is")
	})

	t.Run("errors when core.extension.yml is missing", func(t *testing.T) {
		installer, _, drush, _ := newTestInstaller(t, coreExtensionWithSqlite, "")
		drush.EXPECT().GetConfigSyncDir(t.Context(), "/project", "site1", false).Return("/nowhere", nil)

		_, err := installer.isSqliteModuleEnabled(t.Context(), "/project", "site1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to read core extension file")
		require.ErrorIs(t, err, os.ErrNotExist, "the underlying read error must stay unwrappable")
	})

	t.Run("errors on malformed YAML", func(t *testing.T) {
		installer, _, drush, _ := newTestInstaller(t, "- a\n- b\n", "")
		drush.EXPECT().GetConfigSyncDir(t.Context(), "/project", "site1", false).Return("/config/sync", nil)

		_, err := installer.isSqliteModuleEnabled(t.Context(), "/project", "site1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to unmarshal core extension file")
		var typeErr *yaml.TypeError
		require.ErrorAs(t, err, &typeErr, "the yaml error must stay unwrappable")
	})

	t.Run("errors when there is no module section", func(t *testing.T) {
		installer, _, drush, _ := newTestInstaller(t, "theme:\n  stark: 0\n", "")
		drush.EXPECT().GetConfigSyncDir(t.Context(), "/project", "site1", false).Return("/config/sync", nil)

		_, err := installer.isSqliteModuleEnabled(t.Context(), "/project", "site1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "has no module section")
	})
}

func TestAddSqliteModuleErrors(t *testing.T) {
	t.Run("config sync dir lookup fails", func(t *testing.T) {
		installer, _, drush, _ := newTestInstaller(t, coreExtensionWithoutSqlite, "")
		drush.EXPECT().GetConfigSyncDir(t.Context(), "/project", "site1", false).Return("", assert.AnError)

		require.Error(t, installer.addSqliteModule(t.Context(), "/project", "site1"))
	})

	t.Run("core.extension.yml is missing", func(t *testing.T) {
		installer, _, drush, _ := newTestInstaller(t, coreExtensionWithoutSqlite, "")
		drush.EXPECT().GetConfigSyncDir(t.Context(), "/project", "site1", false).Return("/nowhere", nil)

		err := installer.addSqliteModule(t.Context(), "/project", "site1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to read core extension file")
		require.ErrorIs(t, err, os.ErrNotExist, "the underlying read error must stay unwrappable")
	})

	t.Run("malformed YAML", func(t *testing.T) {
		installer, _, drush, _ := newTestInstaller(t, "- a\n- b\n", "")
		drush.EXPECT().GetConfigSyncDir(t.Context(), "/project", "site1", false).Return("/config/sync", nil)

		err := installer.addSqliteModule(t.Context(), "/project", "site1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to unmarshal core extension file")
		var typeErr *yaml.TypeError
		require.ErrorAs(t, err, &typeErr, "the yaml error must stay unwrappable")
	})

	t.Run("no module section", func(t *testing.T) {
		installer, _, drush, _ := newTestInstaller(t, "theme:\n  stark: 0\n", "")
		drush.EXPECT().GetConfigSyncDir(t.Context(), "/project", "site1", false).Return("/config/sync", nil)

		err := installer.addSqliteModule(t.Context(), "/project", "site1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "has no module section")
	})
}

func TestRemoveProfileErrors(t *testing.T) {
	t.Run("config sync dir lookup fails", func(t *testing.T) {
		installer, _, drush, _ := newTestInstaller(t, coreExtensionWithSqlite, "")
		drush.EXPECT().GetConfigSyncDir(t.Context(), "/project", "site1", false).Return("", assert.AnError)

		require.Error(t, installer.RemoveProfile(t.Context(), "/project", "site1"))
	})

	t.Run("core.extension.yml is missing", func(t *testing.T) {
		installer, _, drush, _ := newTestInstaller(t, coreExtensionWithSqlite, "")
		drush.EXPECT().GetConfigSyncDir(t.Context(), "/project", "site1", false).Return("/nowhere", nil)

		require.Error(t, installer.RemoveProfile(t.Context(), "/project", "site1"))
	})

	t.Run("a profile line that is not the configured one is kept", func(t *testing.T) {
		installer, fs, drush, _ := newTestInstaller(t, "module:\n  node: 0\n  thunder: 1000\nprofile: thunder\n", "")
		drush.EXPECT().GetConfigSyncDir(t.Context(), "/project", "site1", false).Return("/config/sync", nil)

		require.NoError(t, installer.RemoveProfile(t.Context(), "/project", "site1"))

		out, err := afero.ReadFile(fs, "/config/sync/core.extension.yml")
		require.NoError(t, err)
		assert.Contains(t, string(out), "profile: thunder")
	})
}

func TestConfigureDatabaseRestoresSqliteOnAReusedWorkingCopy(t *testing.T) {
	// Regression: settings.php keeps the marker across runs because it is never committed,
	// while core.extension.yml comes back from git without the sqlite entry. A reused working
	// copy therefore has the marker set and the module gone, and guarding the module entry
	// behind the marker left the site uninstallable.
	settingsFromEarlierRun := "<?php\n\n" + settingsMarker + "\n$databases['default']['default'] = [];\n"
	installer, fs, drush, composer := newTestInstaller(t, coreExtensionWithoutSqlite, settingsFromEarlierRun)

	composer.EXPECT().GetConfig(t.Context(), "/project", "extra.drupal-scaffold.locations.web-root").Return("web", nil)
	drush.EXPECT().GetConfigSyncDir(t.Context(), "/project", "site1", false).Return("/config/sync", nil)

	require.NoError(t, installer.ConfigureDatabase(t.Context(), "/project", "site1"))

	core, err := afero.ReadFile(fs, "/config/sync/core.extension.yml")
	require.NoError(t, err)
	var parsed map[string]any
	require.NoError(t, yaml.Unmarshal(core, &parsed))
	assert.Equal(t, 0, parsed["module"].(map[string]any)["sqlite"],
		"sqlite must be put back even though settings.php was already configured")

	// The append stays skipped: the marker still does its original job.
	after, err := afero.ReadFile(fs, "/project/web/sites/site1/settings.php")
	require.NoError(t, err)
	assert.Equal(t, settingsFromEarlierRun, string(after),
		"settings.php must not gain a second database block")
}
