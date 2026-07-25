package drupal

import (
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
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

	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/config/sync/core.extension.yml", []byte(coreExtension), 0o644))
	require.NoError(t, afero.WriteFile(fs, "/project/web/sites/site1/settings.php", []byte(settings), 0o644))

	drush := NewMockDrush(t)
	composer := NewMockComposer(t)

	return &Installer{
		logger:   zap.NewNop(),
		drush:    drush,
		composer: composer,
		fs:       fs,
	}, fs, drush, composer
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

func TestConfigureDatabaseIsIdempotent(t *testing.T) {
	// The run configures each site twice — once for the baseline install, once before the
	// update hooks — against the same settings.php. The second call must not append a second
	// copy of the database, hash_salt and private-path settings.
	installer, fs, drush, composer := newTestInstaller(t, coreExtensionWithSqlite, "<?php\n")

	composer.EXPECT().GetConfig(mock.Anything, "/project", "extra.drupal-scaffold.locations.web-root").Return("web", nil)
	drush.EXPECT().GetConfigSyncDir(mock.Anything, "/project", "site1", false).Return("/config/sync", nil)

	require.NoError(t, installer.ConfigureDatabase(t.Context(), "/project", "site1"))

	after, err := afero.ReadFile(fs, "/project/web/sites/site1/settings.php")
	require.NoError(t, err)
	first := string(after)
	assert.Equal(t, 1, strings.Count(first, settingsMarker))
	assert.Equal(t, 1, strings.Count(first, "$settings['hash_salt']"))

	// Second call: the marker is present, so it must return early without touching the file.
	// GetConfig is still needed to locate settings.php; GetConfigSyncDir is not reached.
	require.NoError(t, installer.ConfigureDatabase(t.Context(), "/project", "site1"))

	after, err = afero.ReadFile(fs, "/project/web/sites/site1/settings.php")
	require.NoError(t, err)
	assert.Equal(t, first, string(after), "settings.php must be unchanged by the second call")
}

func TestConfigureDatabaseEnablesSqliteWhenMissing(t *testing.T) {
	installer, fs, drush, composer := newTestInstaller(t, coreExtensionWithoutSqlite, "<?php\n")

	composer.EXPECT().GetConfig(mock.Anything, "/project", "extra.drupal-scaffold.locations.web-root").Return("web/", nil)
	drush.EXPECT().GetConfigSyncDir(mock.Anything, "/project", "site1", false).Return("/config/sync", nil)

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
		composer.EXPECT().GetConfig(mock.Anything, "/project", "extra.drupal-scaffold.locations.web-root").
			Return("", assert.AnError)

		err := installer.ConfigureDatabase(t.Context(), "/project", "site1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get Drupal web dir")
	})

	t.Run("enabling sqlite fails", func(t *testing.T) {
		installer, _, drush, composer := newTestInstaller(t, coreExtensionWithoutSqlite, "<?php\n")
		composer.EXPECT().GetConfig(mock.Anything, "/project", "extra.drupal-scaffold.locations.web-root").Return("web", nil)
		// The first lookup (isSqliteModuleEnabled) succeeds, addSqliteModule then fails.
		drush.EXPECT().GetConfigSyncDir(mock.Anything, "/project", "site1", false).Return("/config/sync", nil).Once()
		drush.EXPECT().GetConfigSyncDir(mock.Anything, "/project", "site1", false).Return("", assert.AnError).Once()

		err := installer.ConfigureDatabase(t.Context(), "/project", "site1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to enable sqlite module")
	})

	t.Run("settings.php does not exist", func(t *testing.T) {
		// The snippet is an append to an existing settings.php; on its own it is not a valid
		// settings file, so a missing one is an error rather than something to create.
		installer, fs, drush, composer := newTestInstaller(t, coreExtensionWithSqlite, "<?php\n")
		require.NoError(t, fs.Remove("/project/web/sites/site1/settings.php"))

		composer.EXPECT().GetConfig(mock.Anything, "/project", "extra.drupal-scaffold.locations.web-root").Return("web", nil)
		drush.EXPECT().GetConfigSyncDir(mock.Anything, "/project", "site1", false).Return("/config/sync", nil)

		err := installer.ConfigureDatabase(t.Context(), "/project", "site1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to open settings file")
	})
}

func TestIsSqliteModuleEnabled(t *testing.T) {
	t.Run("reports enabled when listed with weight 0", func(t *testing.T) {
		installer, _, drush, _ := newTestInstaller(t, coreExtensionWithSqlite, "")
		drush.EXPECT().GetConfigSyncDir(mock.Anything, "/project", "site1", false).Return("/config/sync", nil)

		enabled, err := installer.isSqliteModuleEnabled(t.Context(), "/project", "site1")
		require.NoError(t, err)
		assert.True(t, enabled)
	})

	t.Run("reports not enabled when absent", func(t *testing.T) {
		installer, _, drush, _ := newTestInstaller(t, coreExtensionWithoutSqlite, "")
		drush.EXPECT().GetConfigSyncDir(mock.Anything, "/project", "site1", false).Return("/config/sync", nil)

		enabled, err := installer.isSqliteModuleEnabled(t.Context(), "/project", "site1")
		require.NoError(t, err)
		assert.False(t, enabled)
	})

	t.Run("errors when the config sync dir cannot be resolved", func(t *testing.T) {
		installer, _, drush, _ := newTestInstaller(t, coreExtensionWithSqlite, "")
		drush.EXPECT().GetConfigSyncDir(mock.Anything, "/project", "site1", false).Return("", assert.AnError)

		_, err := installer.isSqliteModuleEnabled(t.Context(), "/project", "site1")
		require.Error(t, err)
	})

	t.Run("errors when core.extension.yml is missing", func(t *testing.T) {
		installer, _, drush, _ := newTestInstaller(t, coreExtensionWithSqlite, "")
		drush.EXPECT().GetConfigSyncDir(mock.Anything, "/project", "site1", false).Return("/nowhere", nil)

		_, err := installer.isSqliteModuleEnabled(t.Context(), "/project", "site1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to read core extension file")
	})

	t.Run("errors on malformed YAML", func(t *testing.T) {
		installer, _, drush, _ := newTestInstaller(t, "\tnot: [valid", "")
		drush.EXPECT().GetConfigSyncDir(mock.Anything, "/project", "site1", false).Return("/config/sync", nil)

		_, err := installer.isSqliteModuleEnabled(t.Context(), "/project", "site1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to unmarshal core extension file")
	})

	t.Run("errors when there is no module section", func(t *testing.T) {
		installer, _, drush, _ := newTestInstaller(t, "theme:\n  stark: 0\n", "")
		drush.EXPECT().GetConfigSyncDir(mock.Anything, "/project", "site1", false).Return("/config/sync", nil)

		_, err := installer.isSqliteModuleEnabled(t.Context(), "/project", "site1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "has no module section")
	})
}

func TestAddSqliteModuleErrors(t *testing.T) {
	t.Run("config sync dir lookup fails", func(t *testing.T) {
		installer, _, drush, _ := newTestInstaller(t, coreExtensionWithoutSqlite, "")
		drush.EXPECT().GetConfigSyncDir(mock.Anything, "/project", "site1", false).Return("", assert.AnError)

		require.Error(t, installer.addSqliteModule(t.Context(), "/project", "site1"))
	})

	t.Run("core.extension.yml is missing", func(t *testing.T) {
		installer, _, drush, _ := newTestInstaller(t, coreExtensionWithoutSqlite, "")
		drush.EXPECT().GetConfigSyncDir(mock.Anything, "/project", "site1", false).Return("/nowhere", nil)

		err := installer.addSqliteModule(t.Context(), "/project", "site1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to read core extension file")
	})

	t.Run("malformed YAML", func(t *testing.T) {
		installer, _, drush, _ := newTestInstaller(t, "\tnot: [valid", "")
		drush.EXPECT().GetConfigSyncDir(mock.Anything, "/project", "site1", false).Return("/config/sync", nil)

		err := installer.addSqliteModule(t.Context(), "/project", "site1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to unmarshal core extension file")
	})

	t.Run("no module section", func(t *testing.T) {
		installer, _, drush, _ := newTestInstaller(t, "theme:\n  stark: 0\n", "")
		drush.EXPECT().GetConfigSyncDir(mock.Anything, "/project", "site1", false).Return("/config/sync", nil)

		err := installer.addSqliteModule(t.Context(), "/project", "site1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "has no module section")
	})
}

func TestRemoveProfileErrors(t *testing.T) {
	t.Run("config sync dir lookup fails", func(t *testing.T) {
		installer, _, drush, _ := newTestInstaller(t, coreExtensionWithSqlite, "")
		drush.EXPECT().GetConfigSyncDir(mock.Anything, "/project", "site1", false).Return("", assert.AnError)

		require.Error(t, installer.RemoveProfile(t.Context(), "/project", "site1"))
	})

	t.Run("core.extension.yml is missing", func(t *testing.T) {
		installer, _, drush, _ := newTestInstaller(t, coreExtensionWithSqlite, "")
		drush.EXPECT().GetConfigSyncDir(mock.Anything, "/project", "site1", false).Return("/nowhere", nil)

		require.Error(t, installer.RemoveProfile(t.Context(), "/project", "site1"))
	})

	t.Run("a profile line that is not the configured one is kept", func(t *testing.T) {
		installer, fs, drush, _ := newTestInstaller(t, "module:\n  node: 0\n  thunder: 1000\nprofile: thunder\n", "")
		drush.EXPECT().GetConfigSyncDir(mock.Anything, "/project", "site1", false).Return("/config/sync", nil)

		require.NoError(t, installer.RemoveProfile(t.Context(), "/project", "site1"))

		out, err := afero.ReadFile(fs, "/config/sync/core.extension.yml")
		require.NoError(t, err)
		assert.Contains(t, string(out), "profile: thunder")
	})
}
