package drupal

import (
	"os"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// failingFs fails one chosen filesystem operation and delegates the rest. The installer's I/O
// error branches -- a settings.php that opens but cannot be written, a core.extension.yml that
// reads short, a rewrite that cannot be flushed -- are only reached on a full disk or a
// permission change mid-run, so an in-memory filesystem alone never exercises them.
type failingFs struct {
	afero.Fs
	failWrite  bool // writes to an opened file fail
	failClose  bool // closing an opened file fails
	failRead   bool // reads from an opened file fail
	failCreate bool // Create fails outright
}

func (f *failingFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	file, err := f.Fs.OpenFile(name, flag, perm)
	if err != nil {
		return file, err
	}
	return f.wrap(file), nil
}

func (f *failingFs) Open(name string) (afero.File, error) {
	file, err := f.Fs.Open(name)
	if err != nil {
		return file, err
	}
	return f.wrap(file), nil
}

func (f *failingFs) Create(name string) (afero.File, error) {
	if f.failCreate {
		return nil, assert.AnError
	}
	file, err := f.Fs.Create(name)
	if err != nil {
		return file, err
	}
	return f.wrap(file), nil
}

func (f *failingFs) wrap(file afero.File) afero.File {
	if !f.failWrite && !f.failClose && !f.failRead {
		return file
	}
	return &failingFile{File: file, write: f.failWrite, close: f.failClose, read: f.failRead}
}

type failingFile struct {
	afero.File
	write bool
	close bool
	read  bool
}

func (f *failingFile) Write(p []byte) (int, error) {
	if f.write {
		return 0, assert.AnError
	}
	return f.File.Write(p)
}

func (f *failingFile) Read(p []byte) (int, error) {
	if f.read {
		return 0, assert.AnError
	}
	return f.File.Read(p)
}

func (f *failingFile) Close() error {
	err := f.File.Close()
	if f.close {
		return assert.AnError
	}
	return err
}

// newFailingInstaller builds an installer whose filesystem fails as configured, with the
// mocks wired for a ConfigureDatabase run that gets as far as writing settings.php.
func newFailingInstaller(t *testing.T, fs afero.Fs, coreExtension string) (*Installer, *MockDrush, *MockComposer) {
	t.Helper()

	base := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(base, "/config/sync/core.extension.yml", []byte(coreExtension), 0o644))
	require.NoError(t, afero.WriteFile(base, "/project/web/sites/site1/settings.php", []byte("<?php\n"), 0o644))

	// Layer the failing wrapper over the prepared content, so the fixtures themselves are
	// written through a working filesystem.
	if ff, ok := fs.(*failingFs); ok {
		ff.Fs = base
	}

	drush := NewMockDrush(t)
	composer := NewMockComposer(t)

	return &Installer{logger: zap.NewNop(), drush: drush, composer: composer, fs: fs}, drush, composer
}

func TestConfigureDatabaseIOFailures(t *testing.T) {
	t.Run("settings.php cannot be written", func(t *testing.T) {
		installer, drush, composer := newFailingInstaller(t, &failingFs{failWrite: true}, coreExtensionWithSqlite)
		composer.EXPECT().GetConfig(t.Context(), "/project", "extra.drupal-scaffold.locations.web-root").Return("web", nil)
		drush.EXPECT().GetConfigSyncDir(t.Context(), "/project", "site1", false).Return("/config/sync", nil)

		err := installer.ConfigureDatabase(t.Context(), "/project", "site1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to write settings")
		require.ErrorIs(t, err, assert.AnError)
	})

	t.Run("settings.php cannot be closed", func(t *testing.T) {
		// The close error matters on its own: a buffered write that only fails at close would
		// otherwise look like a successful configuration.
		installer, drush, composer := newFailingInstaller(t, &failingFs{failClose: true}, coreExtensionWithSqlite)
		composer.EXPECT().GetConfig(t.Context(), "/project", "extra.drupal-scaffold.locations.web-root").Return("web", nil)
		drush.EXPECT().GetConfigSyncDir(t.Context(), "/project", "site1", false).Return("/config/sync", nil)

		err := installer.ConfigureDatabase(t.Context(), "/project", "site1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to close settings file")
		require.ErrorIs(t, err, assert.AnError)
	})

	t.Run("missing settings.php names the open failure", func(t *testing.T) {
		installer, drush, composer := newFailingInstaller(t, &failingFs{}, coreExtensionWithSqlite)
		require.NoError(t, installer.fs.Remove("/project/web/sites/site1/settings.php"))
		composer.EXPECT().GetConfig(t.Context(), "/project", "extra.drupal-scaffold.locations.web-root").Return("web", nil)
		drush.EXPECT().GetConfigSyncDir(t.Context(), "/project", "site1", false).Return("/config/sync", nil)

		err := installer.ConfigureDatabase(t.Context(), "/project", "site1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to open settings file")
		require.ErrorIs(t, err, os.ErrNotExist, "the open error must survive the wrapper")
	})
}

func TestAddSqliteModuleWriteFailure(t *testing.T) {
	installer, drush, _ := newFailingInstaller(t, &failingFs{failWrite: true}, coreExtensionWithoutSqlite)
	drush.EXPECT().GetConfigSyncDir(t.Context(), "/project", "site1", false).Return("/config/sync", nil)

	err := installer.addSqliteModule(t.Context(), "/project", "site1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to write updated core extension file")
	require.ErrorIs(t, err, assert.AnError)
}

func TestRemoveProfileIOFailures(t *testing.T) {
	t.Run("core.extension.yml cannot be read", func(t *testing.T) {
		// A short read must abort the rewrite: continuing would write back a truncated
		// core.extension.yml, silently dropping the project's module list.
		installer, drush, _ := newFailingInstaller(t, &failingFs{failRead: true}, coreExtensionWithSqlite)
		drush.EXPECT().GetConfigSyncDir(t.Context(), "/project", "site1", false).Return("/config/sync", nil)

		err := installer.RemoveProfile(t.Context(), "/project", "site1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to read file")
		require.ErrorIs(t, err, assert.AnError)
	})

	t.Run("the rewritten file cannot be created", func(t *testing.T) {
		installer, drush, _ := newFailingInstaller(t, &failingFs{failCreate: true}, coreExtensionWithSqlite)
		drush.EXPECT().GetConfigSyncDir(t.Context(), "/project", "site1", false).Return("/config/sync", nil)

		err := installer.RemoveProfile(t.Context(), "/project", "site1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create file")
		require.ErrorIs(t, err, assert.AnError)
	})

	t.Run("the rewritten file cannot be flushed", func(t *testing.T) {
		installer, drush, _ := newFailingInstaller(t, &failingFs{failWrite: true}, coreExtensionWithSqlite)
		drush.EXPECT().GetConfigSyncDir(t.Context(), "/project", "site1", false).Return("/config/sync", nil)

		err := installer.RemoveProfile(t.Context(), "/project", "site1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "flush")
		require.ErrorIs(t, err, assert.AnError)
	})

	t.Run("a write larger than the buffer surfaces at WriteString", func(t *testing.T) {
		// bufio only reports a write error once the buffer drains, so a file bigger than the
		// buffer is what reaches the WriteString branch rather than the Flush one.
		big := "module:\n" + strings.Repeat("  filler_module_with_a_long_name: 0\n", 200) + "profile: standard\n"
		installer, drush, _ := newFailingInstaller(t, &failingFs{failWrite: true}, big)
		drush.EXPECT().GetConfigSyncDir(t.Context(), "/project", "site1", false).Return("/config/sync", nil)

		err := installer.RemoveProfile(t.Context(), "/project", "site1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to write file")
		require.ErrorIs(t, err, assert.AnError)
	})
}
