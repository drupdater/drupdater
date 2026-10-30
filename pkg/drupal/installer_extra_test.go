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
		dataDir:  "/data",
	}, fs, drush, composer, logs
}

func TestNewInstaller(t *testing.T) {
	logger := zap.NewNop()
	drush := NewMockDrush(t)
	composer := NewMockComposer(t)

	installer := NewInstaller(logger, drush, composer, "/data")

	require.NotNil(t, installer)
	assert.Equal(t, logger, installer.logger)
	assert.Equal(t, drush, installer.drush)
	assert.Equal(t, composer, installer.composer)
	assert.Equal(t, "/data", installer.dataDir)
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
}

func TestInstallerLooksUpTheConfigSyncDirectoryOncePerSite(t *testing.T) {
	installer, _, drush, composer := newTestInstaller(t, coreExtensionWithoutSqlite, "<?php\n")
	composer.EXPECT().GetConfig(t.Context(), "/project", "extra.drupal-scaffold.locations.web-root").Return("web", nil)
	drush.EXPECT().GetConfigSyncDir(t.Context(), "/project", "site1").Return("/config/sync", nil).Once()
	drush.EXPECT().InstallSite(t.Context(), "/project", "site1").Return(nil)

	require.NoError(t, installer.Install(t.Context(), "/project", "site1"))
	require.NoError(t, installer.ConfigureDatabase(t.Context(), "/project", "site1"))
}

func TestConfigureDatabaseIsIdempotent(t *testing.T) {
	installer, fs, drush, composer := newTestInstaller(t, coreExtensionWithSqlite, "<?php\n")

	composer.EXPECT().GetConfig(t.Context(), "/project", "extra.drupal-scaffold.locations.web-root").Return("web", nil)
	drush.EXPECT().GetConfigSyncDir(t.Context(), "/project", "site1").Return("/config/sync", nil)

	require.NoError(t, installer.ConfigureDatabase(t.Context(), "/project", "site1"))
	first, err := afero.ReadFile(fs, "/project/web/sites/site1/settings.php")
	require.NoError(t, err)
	assert.Equal(t, 1, strings.Count(string(first), settingsMarker))
	assert.Equal(t, 1, strings.Count(string(first), "$settings['hash_salt']"))

	require.NoError(t, installer.ConfigureDatabase(t.Context(), "/project", "site1"))
	second, err := afero.ReadFile(fs, "/project/web/sites/site1/settings.php")
	require.NoError(t, err)
	assert.Equal(t, string(first), string(second))
}

func TestRestoreSettingsPutsBackTheProjectsFileExactly(t *testing.T) {
	const original = "<?php\n\n$settings['config_sync_directory'] = '../config/sync';\n"
	installer, fs, drush, composer, logs := newTestInstallerWithLogs(t, coreExtensionWithSqlite, original)

	composer.EXPECT().GetConfig(t.Context(), "/project", "extra.drupal-scaffold.locations.web-root").Return("web", nil)
	drush.EXPECT().GetConfigSyncDir(t.Context(), "/project", "site1").Return("/config/sync", nil)

	require.NoError(t, installer.ConfigureDatabase(t.Context(), "/project", "site1"))
	configured, err := afero.ReadFile(fs, "/project/web/sites/site1/settings.php")
	require.NoError(t, err)
	assert.Contains(t, string(configured), "'database' => '/data/site1.sqlite'")
	assert.Contains(t, string(configured), "$settings['file_private_path'] = '/data/private/site1';")

	require.NoError(t, installer.RestoreSettings(t.Context(), "/project", "site1"))
	restored, err := afero.ReadFile(fs, "/project/web/sites/site1/settings.php")
	require.NoError(t, err)
	assert.Equal(t, original, string(restored))

	// Debug output is the only trace of which file a run rewrote.
	for _, msg := range []string{"writing settings", "restoring settings"} {
		entries := logs.FilterMessage(msg).All()
		require.Len(t, entries, 1, msg)
		assert.Equal(t, "/project/web/sites/site1/settings.php", entries[0].ContextMap()["path"], msg)
	}
}

func TestRestoreSettingsWithoutABlockDoesNotWrite(t *testing.T) {
	fs := &failingFs{failWrite: true}
	installer, _, composer := newFailingInstaller(t, fs, coreExtensionWithSqlite)
	installer.logger = zap.NewNop()
	composer.EXPECT().GetConfig(t.Context(), "/project", "extra.drupal-scaffold.locations.web-root").Return("web", nil)

	require.NoError(t, installer.RestoreSettings(t.Context(), "/project", "site1"))
}

func TestReconfiguringAfterRestoreKeepsTheSqliteExclusion(t *testing.T) {
	// The first configure adds sqlite to core.extension.yml, so the second sees it enabled. The
	// exclusion must survive anyway, or the update's config export ships the throwaway module.
	installer, fs, drush, composer := newTestInstaller(t, coreExtensionWithoutSqlite, "<?php\n")

	composer.EXPECT().GetConfig(t.Context(), "/project", "extra.drupal-scaffold.locations.web-root").Return("web", nil)
	drush.EXPECT().GetConfigSyncDir(t.Context(), "/project", "site1").Return("/config/sync", nil)

	require.NoError(t, installer.ConfigureDatabase(t.Context(), "/project", "site1"))
	require.NoError(t, installer.RestoreSettings(t.Context(), "/project", "site1"))
	require.NoError(t, installer.ConfigureDatabase(t.Context(), "/project", "site1"))

	settings, err := afero.ReadFile(fs, "/project/web/sites/site1/settings.php")
	require.NoError(t, err)
	assert.Contains(t, string(settings), "$settings['config_exclude_modules'] = ['sqlite'];")
}

func TestStripSettingsBlock(t *testing.T) {
	assert.Equal(t, "<?php\n", string(stripSettingsBlock([]byte("<?php\n\n"+settingsMarker+"\n$x = 1;\n"))))
	assert.Equal(t, "<?php\n", string(stripSettingsBlock([]byte("<?php\n\n\n" + settingsMarker + "\n")))[:6],
		"only the one newline the block starts with is removed")
	assert.Equal(t, "<?php", string(stripSettingsBlock([]byte("<?php\n"+settingsMarker+"\n"))))
	assert.Empty(t, stripSettingsBlock([]byte(settingsMarker+"\n")))
	assert.Equal(t, "<?php", string(stripSettingsBlock([]byte("<?php"+settingsMarker+"\n"))),
		"a character other than a newline before the marker is the project's")
	assert.Equal(t, "<?php\n", string(stripSettingsBlock([]byte("<?php\n"))))
}

func TestConfigureDatabaseEnablesSqliteWhenMissing(t *testing.T) {
	installer, fs, drush, composer := newTestInstaller(t, coreExtensionWithoutSqlite, "<?php\n")

	composer.EXPECT().GetConfig(t.Context(), "/project", "extra.drupal-scaffold.locations.web-root").Return("web/", nil)
	drush.EXPECT().GetConfigSyncDir(t.Context(), "/project", "site1").Return("/config/sync", nil)

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
		installer, fs, drush, composer := newTestInstaller(t, coreExtensionWithoutSqlite, "<?php\n")
		composer.EXPECT().GetConfig(t.Context(), "/project", "extra.drupal-scaffold.locations.web-root").Return("web", nil)
		drush.EXPECT().GetConfigSyncDir(t.Context(), "/project", "site1").Return("/config/sync", nil)
		require.NoError(t, fs.Remove("/config/sync/core.extension.yml"))

		err := installer.ConfigureDatabase(t.Context(), "/project", "site1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to enable sqlite module")
		require.ErrorIs(t, err, os.ErrNotExist)
	})

	t.Run("settings.php does not exist", func(t *testing.T) {
		// The snippet is an append to an existing settings.php; on its own it is not a valid
		// settings file, so a missing one is an error rather than something to create.
		installer, fs, drush, composer := newTestInstaller(t, coreExtensionWithSqlite, "<?php\n")
		require.NoError(t, fs.Remove("/project/web/sites/site1/settings.php"))

		composer.EXPECT().GetConfig(t.Context(), "/project", "extra.drupal-scaffold.locations.web-root").Return("web", nil)
		drush.EXPECT().GetConfigSyncDir(t.Context(), "/project", "site1").Return("/config/sync", nil)

		err := installer.ConfigureDatabase(t.Context(), "/project", "site1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to open settings file")
	})
}

func TestIsSqliteModuleEnabled(t *testing.T) {
	t.Run("reports enabled when listed with weight 0", func(t *testing.T) {
		installer, _, drush, _ := newTestInstaller(t, coreExtensionWithSqlite, "")
		drush.EXPECT().GetConfigSyncDir(t.Context(), "/project", "site1").Return("/config/sync", nil)

		enabled, err := installer.isSqliteModuleEnabled(t.Context(), "/project", "site1")
		require.NoError(t, err)
		assert.True(t, enabled)
	})

	t.Run("reports not enabled when absent", func(t *testing.T) {
		installer, _, drush, _ := newTestInstaller(t, coreExtensionWithoutSqlite, "")
		drush.EXPECT().GetConfigSyncDir(t.Context(), "/project", "site1").Return("/config/sync", nil)

		enabled, err := installer.isSqliteModuleEnabled(t.Context(), "/project", "site1")
		require.NoError(t, err)
		assert.False(t, enabled)
	})

	t.Run("reports not enabled when listed with a non-zero weight", func(t *testing.T) {
		// Both halves of the check matter: the key being present is not enough, its weight has
		// to be 0. Without this case the "exists" and "weight is 0" clauses are interchangeable.
		installer, _, drush, _ := newTestInstaller(t, "module:\n  node: 0\n  sqlite: 10\n", "")
		drush.EXPECT().GetConfigSyncDir(t.Context(), "/project", "site1").Return("/config/sync", nil)

		enabled, err := installer.isSqliteModuleEnabled(t.Context(), "/project", "site1")
		require.NoError(t, err)
		assert.False(t, enabled)
	})

	t.Run("errors when the config sync dir cannot be resolved", func(t *testing.T) {
		installer, _, drush, _ := newTestInstaller(t, coreExtensionWithSqlite, "")
		drush.EXPECT().GetConfigSyncDir(t.Context(), "/project", "site1").Return("", assert.AnError)

		_, err := installer.isSqliteModuleEnabled(t.Context(), "/project", "site1")
		require.Error(t, err)
		require.ErrorIs(t, err, assert.AnError, "the lookup failure must be returned as-is")
	})

	t.Run("errors when core.extension.yml is missing", func(t *testing.T) {
		installer, _, drush, _ := newTestInstaller(t, coreExtensionWithSqlite, "")
		drush.EXPECT().GetConfigSyncDir(t.Context(), "/project", "site1").Return("/nowhere", nil)

		_, err := installer.isSqliteModuleEnabled(t.Context(), "/project", "site1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to read core extension file")
		require.ErrorIs(t, err, os.ErrNotExist, "the underlying read error must stay unwrappable")
	})

	t.Run("errors on malformed YAML", func(t *testing.T) {
		installer, _, drush, _ := newTestInstaller(t, "- a\n- b\n", "")
		drush.EXPECT().GetConfigSyncDir(t.Context(), "/project", "site1").Return("/config/sync", nil)

		_, err := installer.isSqliteModuleEnabled(t.Context(), "/project", "site1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to unmarshal core extension file")
		var typeErr *yaml.TypeError
		require.ErrorAs(t, err, &typeErr, "the yaml error must stay unwrappable")
	})

	t.Run("errors when there is no module section", func(t *testing.T) {
		installer, _, drush, _ := newTestInstaller(t, "theme:\n  stark: 0\n", "")
		drush.EXPECT().GetConfigSyncDir(t.Context(), "/project", "site1").Return("/config/sync", nil)

		_, err := installer.isSqliteModuleEnabled(t.Context(), "/project", "site1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "has no module section")
	})
}

func TestAddSqliteModuleErrors(t *testing.T) {
	t.Run("config sync dir lookup fails", func(t *testing.T) {
		installer, _, drush, _ := newTestInstaller(t, coreExtensionWithoutSqlite, "")
		drush.EXPECT().GetConfigSyncDir(t.Context(), "/project", "site1").Return("", assert.AnError)

		require.Error(t, installer.addSqliteModule(t.Context(), "/project", "site1"))
	})

	t.Run("core.extension.yml is missing", func(t *testing.T) {
		installer, _, drush, _ := newTestInstaller(t, coreExtensionWithoutSqlite, "")
		drush.EXPECT().GetConfigSyncDir(t.Context(), "/project", "site1").Return("/nowhere", nil)

		err := installer.addSqliteModule(t.Context(), "/project", "site1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to read core extension file")
		require.ErrorIs(t, err, os.ErrNotExist, "the underlying read error must stay unwrappable")
	})

	t.Run("malformed YAML", func(t *testing.T) {
		installer, _, drush, _ := newTestInstaller(t, "- a\n- b\n", "")
		drush.EXPECT().GetConfigSyncDir(t.Context(), "/project", "site1").Return("/config/sync", nil)

		err := installer.addSqliteModule(t.Context(), "/project", "site1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to unmarshal core extension file")
		var typeErr *yaml.TypeError
		require.ErrorAs(t, err, &typeErr, "the yaml error must stay unwrappable")
	})

	t.Run("no module section", func(t *testing.T) {
		installer, _, drush, _ := newTestInstaller(t, "theme:\n  stark: 0\n", "")
		drush.EXPECT().GetConfigSyncDir(t.Context(), "/project", "site1").Return("/config/sync", nil)

		err := installer.addSqliteModule(t.Context(), "/project", "site1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "has no module section")
	})
}

func TestRemoveProfileErrors(t *testing.T) {
	t.Run("config sync dir lookup fails", func(t *testing.T) {
		installer, _, drush, _ := newTestInstaller(t, coreExtensionWithSqlite, "")
		drush.EXPECT().GetConfigSyncDir(t.Context(), "/project", "site1").Return("", assert.AnError)

		require.Error(t, installer.RemoveProfile(t.Context(), "/project", "site1"))
	})

	t.Run("core.extension.yml is missing", func(t *testing.T) {
		installer, _, drush, _ := newTestInstaller(t, coreExtensionWithSqlite, "")
		drush.EXPECT().GetConfigSyncDir(t.Context(), "/project", "site1").Return("/nowhere", nil)

		require.Error(t, installer.RemoveProfile(t.Context(), "/project", "site1"))
	})

	t.Run("a profile line that is not the configured one is kept", func(t *testing.T) {
		installer, fs, drush, _ := newTestInstaller(t, "module:\n  node: 0\n  thunder: 1000\nprofile: thunder\n", "")
		drush.EXPECT().GetConfigSyncDir(t.Context(), "/project", "site1").Return("/config/sync", nil)

		require.NoError(t, installer.RemoveProfile(t.Context(), "/project", "site1"))

		out, err := afero.ReadFile(fs, "/config/sync/core.extension.yml")
		require.NoError(t, err)
		assert.Contains(t, string(out), "profile: thunder")
	})
}

func TestConfigureDatabaseOnAReusedWorkingCopy(t *testing.T) {
	// A killed earlier run can leave its block behind, pointing at a data directory that is gone,
	// and core.extension.yml back from git without the sqlite entry.
	settingsFromEarlierRun := "<?php\n\n" + settingsMarker + "\n$databases['default']['default'] = ['database' => '/gone/site1.sqlite'];\n"
	installer, fs, drush, composer := newTestInstaller(t, coreExtensionWithoutSqlite, settingsFromEarlierRun)

	composer.EXPECT().GetConfig(t.Context(), "/project", "extra.drupal-scaffold.locations.web-root").Return("web", nil)
	drush.EXPECT().GetConfigSyncDir(t.Context(), "/project", "site1").Return("/config/sync", nil)

	require.NoError(t, installer.ConfigureDatabase(t.Context(), "/project", "site1"))

	core, err := afero.ReadFile(fs, "/config/sync/core.extension.yml")
	require.NoError(t, err)
	var parsed map[string]any
	require.NoError(t, yaml.Unmarshal(core, &parsed))
	assert.Equal(t, 0, parsed["module"].(map[string]any)["sqlite"])

	after, err := afero.ReadFile(fs, "/project/web/sites/site1/settings.php")
	require.NoError(t, err)
	assert.Equal(t, 1, strings.Count(string(after), settingsMarker))
	assert.NotContains(t, string(after), "/gone/")
	assert.Contains(t, string(after), "/data/site1.sqlite")
}
