package siteinstall

import (
	"os"
	"strings"
	"testing"

	"context"
	"errors"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
	"gopkg.in/yaml.v3"
)

func TestInstallDrupal(t *testing.T) {
	// Setup common test data
	logger := zap.NewNop()
	fs := afero.NewMemMapFs()

	coreExtensionContent := `
_core:
  default_config_hash: 4GIX5Esnc_umpXUBj4IIocRX7Mt5fPhm4AgXfE3E56E
module:
  access_unpublished: 0
  admin_toolbar: 0
  admin_toolbar_links_access_filter: 0
  sqlite: 0
  thunder: 1000
theme:
  gin: 0
profile: thunder
`
	configSyncDir := "/tmp/config/sync"
	coreExtensionPath := configSyncDir + "/core.extension.yml"

	if err := afero.WriteFile(fs, coreExtensionPath, []byte(coreExtensionContent), 0644); err != nil {
		t.Fatalf("Failed to write initial content to core.extension.yml: %v", err)
	}

	settingsContent := ""
	settingsPath := "/tmp/web/sites/site1/settings.php"
	if err := afero.WriteFile(fs, settingsPath, []byte(settingsContent), 0644); err != nil {
		t.Fatalf("Failed to write initial content to settings.php: %v", err)
	}

	t.Run("Success", func(t *testing.T) {
		// Setup mocks
		var duringInstall []byte
		drush := NewMockDrush(t)
		drush.EXPECT().InstallSite(t.Context(), "/tmp", "site1").RunAndReturn(func(context.Context, string, string) error {
			var err error
			duringInstall, err = afero.ReadFile(fs, settingsPath)
			return err
		})
		drush.EXPECT().GetConfigSyncDir(t.Context(), "/tmp", "site1").Return(configSyncDir, nil)

		composer := NewMockComposer(t)
		composer.EXPECT().Project(t.Context(), "/tmp").Return(atWebRoot("web"), nil)

		installer := &Installer{
			logger:   logger,
			drush:    drush,
			composer: composer,
			fs:       fs,
			dataDir:  "/data",
		}

		err := installer.Install(t.Context(), "/tmp", "site1")
		if err != nil {
			t.Fatalf("Failed to install Drupal: %v", err)
		}

		updatedContent := `
` + settingsMarker + `
$databases['default']['default'] = [
	'database' => '/data/site1.sqlite',
	'prefix' => '',
	'driver' => 'sqlite',
	'namespace' => 'Drupal\\sqlite\\Driver\\Database\\sqlite',
	'autoload' => 'core/modules/sqlite/src/Driver/Database/sqlite/',
];
$settings['skip_permissions_hardening'] = TRUE;
$settings['file_private_path'] = '/data/private/site1';
$settings['hash_salt'] = 'changeme';
`

		if string(duringInstall) != updatedContent {
			t.Fatalf("Expected content during install: %s, got: %s", updatedContent, string(duringInstall))
		}

		after, err := afero.ReadFile(fs, settingsPath)
		if err != nil {
			t.Fatalf("Failed to read settings.php: %v", err)
		}
		if string(after) != settingsContent {
			t.Fatalf("settings.php must be restored after the install, got: %s", string(after))
		}
	})

	t.Run("Failure", func(t *testing.T) {
		// Setup mocks
		drush := NewMockDrush(t)
		drush.EXPECT().InstallSite(t.Context(), "/tmp", "site1").Return(errors.New("failed to install site"))
		drush.EXPECT().GetConfigSyncDir(t.Context(), "/tmp", "site1").Return(configSyncDir, nil)

		composer := NewMockComposer(t)
		composer.EXPECT().Project(t.Context(), "/tmp").Return(atWebRoot("web"), nil)

		installer := &Installer{
			logger:   logger,
			drush:    drush,
			composer: composer,
			fs:       fs,
		}

		err := installer.Install(t.Context(), "/tmp", "site1")

		if err == nil {
			t.Fatalf("Expected an error but got nil")
		}

		after, readErr := afero.ReadFile(fs, settingsPath)
		if readErr != nil {
			t.Fatalf("Failed to read settings.php: %v", readErr)
		}
		if string(after) != settingsContent {
			t.Fatalf("settings.php must be restored after a failed install, got: %s", string(after))
		}
	})
}

func TestAddSqliteModule(t *testing.T) {
	logger := zap.NewNop()
	fs := afero.NewMemMapFs()

	// Create a temporary file to act as the core.extension.yml
	tempFile, err := fs.Create("/tmp/core.extension.yml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}

	// Write initial YAML content to the temp file
	initialContent := `
_core:
  default_config_hash: 4GIX5Esnc_umpXUBj4IIocRX7Mt5fPhm4AgXfE3E56E
module:
  access_unpublished: 0
  admin_toolbar: 0
  admin_toolbar_links_access_filter: 0
  thunder: 1000
theme:
  gin: 0
profile: thunder
`
	if _, err := tempFile.Write([]byte(initialContent)); err != nil {
		t.Fatalf("Failed to write initial content to temp file: %v", err)
	}

	// Close the temp file so it can be read by the function
	if err := tempFile.Close(); err != nil {
		t.Fatalf("Failed to close temp file: %v", err)
	}

	installer := &Installer{
		logger: logger,
		fs:     fs,
	}

	if err := installer.addSqliteModule("/tmp/core.extension.yml"); err != nil {
		t.Fatalf("Failed to add SQLite module: %v", err)
	}

	updatedContent, err := afero.ReadFile(fs, tempFile.Name())
	if err != nil {
		t.Fatalf("Failed to read updated content from temp file: %v", err)
	}

	// Unmarshal the updated content
	var updatedConfig map[string]any
	if err := yaml.Unmarshal(updatedContent, &updatedConfig); err != nil {
		t.Fatalf("Failed to unmarshal updated content: %v", err)
	}

	// Check if the SQLite module was added correctly
	modules, ok := updatedConfig["module"].(map[string]any)
	if !ok {
		t.Fatalf("Modules key is not a map")
	}

	if _, exists := modules["sqlite"]; !exists {
		t.Fatalf("SQLite module was not added")
	}

	if modules["sqlite"] != 0 {
		t.Fatalf("SQLite module value is not 0")
	}
}

func TestRemoveProfile(t *testing.T) {
	logger := zap.NewNop()
	fs := afero.NewMemMapFs()

	// Create a temporary file to act as the core.extension.yml
	tempFile, err := fs.Create("/tmp/core.extension.yml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}

	// Write initial YAML content to the temp file
	initialContent := `
_core:
  default_config_hash: 4GIX5Esnc_umpXUBj4IIocRX7Mt5fPhm4AgXfE3E56E
module:
  access_unpublished: 0
  admin_toolbar: 0
  admin_toolbar_links_access_filter: 0
  standard: 1000
theme:
  gin: 0
profile: standard
`
	if _, err := tempFile.Write([]byte(initialContent)); err != nil {
		t.Fatalf("Failed to write initial content to temp file: %v", err)
	}

	// Close the temp file so it can be read by the function
	if err := tempFile.Close(); err != nil {
		t.Fatalf("Failed to close temp file: %v", err)
	}

	installer := &Installer{
		logger: logger,
		fs:     fs,
	}

	if err := installer.removeProfile("/tmp/core.extension.yml"); err != nil {
		t.Fatalf("Failed to remove profile: %v", err)
	}

	expectedContent := `
_core:
  default_config_hash: 4GIX5Esnc_umpXUBj4IIocRX7Mt5fPhm4AgXfE3E56E
module:
  access_unpublished: 0
  admin_toolbar: 0
  admin_toolbar_links_access_filter: 0
theme:
  gin: 0
`

	updatedContent, err := afero.ReadFile(fs, tempFile.Name())
	if err != nil {
		t.Fatalf("Failed to read updated content from temp file: %v", err)
	}

	if string(updatedContent) != expectedContent {
		t.Fatalf("Expected content: %s, got: %s", expectedContent, string(updatedContent))
	}
}

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
	t.Run("config sync dir lookup fails", func(t *testing.T) {
		installer, _, drush, _ := newTestInstaller(t, coreExtensionWithSqlite, "<?php\n")
		drush.EXPECT().GetConfigSyncDir(t.Context(), "/project", "site1").Return("", assert.AnError)

		require.ErrorIs(t, installer.Install(t.Context(), "/project", "site1"), assert.AnError)
	})

	t.Run("database configuration fails", func(t *testing.T) {
		installer, _, drush, composer := newTestInstaller(t, coreExtensionWithSqlite, "<?php\n")
		drush.EXPECT().GetConfigSyncDir(t.Context(), "/project", "site1").Return("/config/sync", nil)
		composer.EXPECT().Project(t.Context(), "/project").Return(atWebRoot(""), assert.AnError)

		// No InstallSite expectation: reaching it would fail the mock.
		err := installer.Install(t.Context(), "/project", "site1")
		require.Error(t, err)
		require.ErrorIs(t, err, assert.AnError)
	})
}

func TestInstallLooksUpTheConfigSyncDirectoryOnce(t *testing.T) {
	installer, _, drush, composer := newTestInstaller(t, coreExtensionWithoutSqlite, "<?php\n")
	composer.EXPECT().Project(t.Context(), "/project").Return(atWebRoot("web"), nil)
	drush.EXPECT().GetConfigSyncDir(t.Context(), "/project", "site1").Return("/config/sync", nil).Once()
	drush.EXPECT().InstallSite(t.Context(), "/project", "site1").Return(nil)

	require.NoError(t, installer.Install(t.Context(), "/project", "site1"))
}

func TestConfigureDatabaseIsIdempotent(t *testing.T) {
	installer, fs, drush, composer := newTestInstaller(t, coreExtensionWithSqlite, "<?php\n")

	composer.EXPECT().Project(t.Context(), "/project").Return(atWebRoot("web"), nil)
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

	composer.EXPECT().Project(t.Context(), "/project").Return(atWebRoot("web"), nil)
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
	composer.EXPECT().Project(t.Context(), "/project").Return(atWebRoot("web"), nil)

	require.NoError(t, installer.RestoreSettings(t.Context(), "/project", "site1"))
}

func TestReconfiguringAfterRestoreKeepsTheSqliteExclusion(t *testing.T) {
	// The first configure adds sqlite to core.extension.yml, so the second sees it enabled. The
	// exclusion must survive anyway, or the update's config export ships the throwaway module.
	installer, fs, drush, composer := newTestInstaller(t, coreExtensionWithoutSqlite, "<?php\n")

	composer.EXPECT().Project(t.Context(), "/project").Return(atWebRoot("web"), nil)
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

	composer.EXPECT().Project(t.Context(), "/project").Return(atWebRoot("web"), nil)
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
	t.Run("config sync dir lookup fails", func(t *testing.T) {
		installer, _, drush, _ := newTestInstaller(t, coreExtensionWithSqlite, "<?php\n")
		drush.EXPECT().GetConfigSyncDir(t.Context(), "/project", "site1").Return("", assert.AnError)

		require.ErrorIs(t, installer.ConfigureDatabase(t.Context(), "/project", "site1"), assert.AnError)
	})

	t.Run("web root lookup fails", func(t *testing.T) {
		installer, _, drush, composer := newTestInstaller(t, coreExtensionWithSqlite, "<?php\n")
		drush.EXPECT().GetConfigSyncDir(t.Context(), "/project", "site1").Return("/config/sync", nil)
		composer.EXPECT().Project(t.Context(), "/project").Return(atWebRoot(""), assert.AnError)

		err := installer.ConfigureDatabase(t.Context(), "/project", "site1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get Drupal web dir")
		require.ErrorIs(t, err, assert.AnError, "the cause must survive the wrapper")
	})

	t.Run("enabling sqlite fails", func(t *testing.T) {
		installer, fs, drush, composer := newTestInstaller(t, coreExtensionWithoutSqlite, "<?php\n")
		composer.EXPECT().Project(t.Context(), "/project").Return(atWebRoot("web"), nil)
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

		composer.EXPECT().Project(t.Context(), "/project").Return(atWebRoot("web"), nil)
		drush.EXPECT().GetConfigSyncDir(t.Context(), "/project", "site1").Return("/config/sync", nil)

		err := installer.ConfigureDatabase(t.Context(), "/project", "site1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to open settings file")
	})
}

func TestIsSqliteModuleEnabled(t *testing.T) {
	t.Run("reports enabled when listed with weight 0", func(t *testing.T) {
		installer, _, _, _ := newTestInstaller(t, coreExtensionWithSqlite, "")

		enabled, err := installer.isSqliteModuleEnabled("/config/sync/core.extension.yml")
		require.NoError(t, err)
		assert.True(t, enabled)
	})

	t.Run("reports not enabled when absent", func(t *testing.T) {
		installer, _, _, _ := newTestInstaller(t, coreExtensionWithoutSqlite, "")

		enabled, err := installer.isSqliteModuleEnabled("/config/sync/core.extension.yml")
		require.NoError(t, err)
		assert.False(t, enabled)
	})

	t.Run("reports not enabled when listed with a non-zero weight", func(t *testing.T) {
		// Both halves of the check matter: the key being present is not enough, its weight has
		// to be 0. Without this case the "exists" and "weight is 0" clauses are interchangeable.
		installer, _, _, _ := newTestInstaller(t, "module:\n  node: 0\n  sqlite: 10\n", "")

		enabled, err := installer.isSqliteModuleEnabled("/config/sync/core.extension.yml")
		require.NoError(t, err)
		assert.False(t, enabled)
	})

	t.Run("errors when core.extension.yml is missing", func(t *testing.T) {
		installer, _, _, _ := newTestInstaller(t, coreExtensionWithSqlite, "")

		_, err := installer.isSqliteModuleEnabled("/nowhere/core.extension.yml")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to read core extension file")
		require.ErrorIs(t, err, os.ErrNotExist, "the underlying read error must stay unwrappable")
	})

	t.Run("errors on malformed YAML", func(t *testing.T) {
		installer, _, _, _ := newTestInstaller(t, "- a\n- b\n", "")

		_, err := installer.isSqliteModuleEnabled("/config/sync/core.extension.yml")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to unmarshal core extension file")
		var typeErr *yaml.TypeError
		require.ErrorAs(t, err, &typeErr, "the yaml error must stay unwrappable")
	})

	t.Run("errors when there is no module section", func(t *testing.T) {
		installer, _, _, _ := newTestInstaller(t, "theme:\n  stark: 0\n", "")

		_, err := installer.isSqliteModuleEnabled("/config/sync/core.extension.yml")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "has no module section")
	})
}

func TestAddSqliteModuleErrors(t *testing.T) {

	t.Run("core.extension.yml is missing", func(t *testing.T) {
		installer, _, _, _ := newTestInstaller(t, coreExtensionWithoutSqlite, "")

		err := installer.addSqliteModule("/nowhere/core.extension.yml")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to read core extension file")
		require.ErrorIs(t, err, os.ErrNotExist, "the underlying read error must stay unwrappable")
	})

	t.Run("malformed YAML", func(t *testing.T) {
		installer, _, _, _ := newTestInstaller(t, "- a\n- b\n", "")

		err := installer.addSqliteModule("/config/sync/core.extension.yml")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to unmarshal core extension file")
		var typeErr *yaml.TypeError
		require.ErrorAs(t, err, &typeErr, "the yaml error must stay unwrappable")
	})

	t.Run("no module section", func(t *testing.T) {
		installer, _, _, _ := newTestInstaller(t, "theme:\n  stark: 0\n", "")

		err := installer.addSqliteModule("/config/sync/core.extension.yml")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "has no module section")
	})
}

func TestRemoveProfileErrors(t *testing.T) {

	t.Run("core.extension.yml is missing", func(t *testing.T) {
		installer, _, _, _ := newTestInstaller(t, coreExtensionWithSqlite, "")

		require.Error(t, installer.removeProfile("/nowhere/core.extension.yml"))
	})

	t.Run("a profile line that is not the configured one is kept", func(t *testing.T) {
		installer, fs, _, _ := newTestInstaller(t, "module:\n  node: 0\n  thunder: 1000\nprofile: thunder\n", "")

		require.NoError(t, installer.removeProfile("/config/sync/core.extension.yml"))

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

	composer.EXPECT().Project(t.Context(), "/project").Return(atWebRoot("web"), nil)
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
