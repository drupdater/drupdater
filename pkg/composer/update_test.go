package composer

import (
	"context"
	"fmt"
	"github.com/drupdater/drupdater/pkg/php"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

const (
	baseLock = `{"content-hash": "base"}`
	newLock  = `{"content-hash": "new"}`
	baseJSON = `{"require": {"drupal/core": "^10.2"}}`
	newJSON  = `{"require": {"drupal/core": "^10.3"}}`
)

// fakeUpdate stands in for composer: `update` writes newJSON and newLock into its directory (or
// fails, with updateFails), and `diff` prints diffOut (or fails, with diffFails).
type fakeUpdate struct {
	diffOut     string
	updateFails bool
	diffFails   bool

	mu        sync.Mutex
	calls     [][]string
	baseLocks []string
}

func (f *fakeUpdate) command(t *testing.T) php.CommandFactory {
	t.Helper()
	return func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
		f.mu.Lock()
		f.calls = append(f.calls, arg)
		// Read now, while Update still holds the base lock on disk for the subprocess.
		for _, a := range arg {
			if path, ok := strings.CutPrefix(a, "--base="); ok {
				content, err := os.ReadFile(path)
				require.NoError(t, err)
				f.baseLocks = append(f.baseLocks, string(content))
			}
		}
		f.mu.Unlock()

		cs := append([]string{"-test.run=TestUpdateHelperProcess", "--"}, arg...)
		cmd := exec.CommandContext(ctx, os.Args[0], cs...) //nolint:gosec // test helper process
		cmd.Env = []string{
			"GO_WANT_UPDATE_HELPER=1", "GOCOVERDIR=/tmp",
			"HELPER_DIFF=" + f.diffOut,
			fmt.Sprintf("HELPER_UPDATE_FAILS=%t", f.updateFails),
			fmt.Sprintf("HELPER_DIFF_FAILS=%t", f.diffFails),
		}
		return cmd
	}
}

func (f *fakeUpdate) call(t *testing.T, subcommand string) []string {
	t.Helper()
	for _, c := range f.calls {
		if c[0] == subcommand {
			return c
		}
	}
	t.Fatalf("composer %s was never run; calls: %v", subcommand, f.calls)
	return nil
}

func TestUpdateHelperProcess(*testing.T) {
	if os.Getenv("GO_WANT_UPDATE_HELPER") != "1" {
		return
	}
	args := os.Args[slices.Index(os.Args, "--")+1:]
	switch args[0] {
	case "update":
		if os.Getenv("HELPER_UPDATE_FAILS") == "true" {
			fmt.Println("Your requirements could not be resolved")
			os.Exit(1)
		}
		_ = os.WriteFile("composer.json", []byte(newJSON), 0o600)
		_ = os.WriteFile("composer.lock", []byte(newLock), 0o600)
	case "diff":
		if os.Getenv("HELPER_DIFF_FAILS") == "true" {
			os.Exit(1)
		}
		fmt.Println(os.Getenv("HELPER_DIFF"))
	}
	os.Exit(0)
}

func newUpdateProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "composer.json"), []byte(baseJSON), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "composer.lock"), []byte(baseLock), 0o600))
	return dir
}

func readFile(t *testing.T, dir, name string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(dir, name))
	require.NoError(t, err)
	return string(content)
}

func newUpdateCLI(newCommand php.CommandFactory) *CLI {
	return &CLI{logger: zap.NewNop(), fs: afero.NewOsFs(), newCommand: newCommand}
}

// Shaped like composer-diff's real output, dev-branch commit and all.
const diffWithEveryOperation = `{
	"packages": {
		"drupal/core": {"name": "drupal/core", "operation": "upgrade", "version_base": "10.2.0", "version_target": "10.3.1"},
		"drupal/token": {"name": "drupal/token", "operation": "upgrade", "version_base": "1.11.0", "version_target": "1.13.0"},
		"symfony/polyfill-ctype": {"name": "symfony/polyfill-ctype", "operation": "change", "version_base": "1.x-dev 0424dff", "version_target": "1.x-dev 141046a"},
		"drupal/webform": {"name": "drupal/webform", "operation": "downgrade", "version_base": "6.2.0", "version_target": "6.1.5"},
		"drupal/legacy": {"name": "drupal/legacy", "operation": "remove", "version_base": "1.0.0", "version_target": null},
		"drupal/new_module": {"name": "drupal/new_module", "operation": "install", "version_base": null, "version_target": "1.2.3"}
	},
	"packages-dev": {
		"behat/mink": {"name": "behat/mink", "operation": "upgrade", "version_base": "v1.11.0", "version_target": "v1.12.0"},
		"behat/foo": {"name": "behat/foo", "operation": "install", "version_base": null, "version_target": "v1.0.0"}
	}
}`

func TestUpdateReadsChangesFromTheLockDiff(t *testing.T) {
	dir := newUpdateProject(t)
	fake := &fakeUpdate{diffOut: diffWithEveryOperation}

	changes, err := newUpdateCLI(fake.command(t)).Update(t.Context(), dir, nil, nil, false, false)
	require.NoError(t, err)

	assert.Equal(t, []PackageChange{
		{Action: "Upgrade", Package: "behat/mink", From: "v1.11.0", To: "v1.12.0"},
		{Action: "Upgrade", Package: "drupal/core", From: "10.2.0", To: "10.3.1"},
		{Action: "Upgrade", Package: "drupal/token", From: "1.11.0", To: "1.13.0"},
		{Action: "Upgrade", Package: "symfony/polyfill-ctype", From: "1.x-dev 0424dff", To: "1.x-dev 141046a"},
		{Action: "Downgrade", Package: "drupal/webform", From: "6.2.0", To: "6.1.5"},
		{Action: "Remove", Package: "drupal/legacy", From: "1.0.0"},
		{Action: "Install", Package: "behat/foo", To: "v1.0.0"},
		{Action: "Install", Package: "drupal/new_module", To: "1.2.3"},
	}, changes)
}

func TestUpdateReportsAPackageMovedBetweenRequireAndRequireDevOnce(t *testing.T) {
	dir := newUpdateProject(t)
	fake := &fakeUpdate{diffOut: `{
		"packages": {
			"justinrainbow/json-schema": {"name": "justinrainbow/json-schema", "operation": "install", "version_base": null, "version_target": "6.8.2"},
			"symfony/process": {"name": "symfony/process", "operation": "install", "version_base": null, "version_target": "v7.1.0"},
			"drupal/dev_branch": {"name": "drupal/dev_branch", "operation": "install", "version_base": null, "version_target": "1.x-dev 141046a"},
			"drupal/same": {"name": "drupal/same", "operation": "install", "version_base": null, "version_target": "2.0.0"}
		},
		"packages-dev": {
			"justinrainbow/json-schema": {"name": "justinrainbow/json-schema", "operation": "remove", "version_base": "6.10.0", "version_target": null},
			"symfony/process": {"name": "symfony/process", "operation": "remove", "version_base": "v7.0.9", "version_target": null},
			"drupal/dev_branch": {"name": "drupal/dev_branch", "operation": "remove", "version_base": "1.x-dev 0424dff", "version_target": null},
			"drupal/same": {"name": "drupal/same", "operation": "remove", "version_base": "2.0.0", "version_target": null}
		}
	}`}

	changes, err := newUpdateCLI(fake.command(t)).Update(t.Context(), dir, nil, nil, false, false)
	require.NoError(t, err)

	assert.Equal(t, []PackageChange{
		{Action: "Upgrade", Package: "drupal/dev_branch", From: "1.x-dev 0424dff", To: "1.x-dev 141046a"},
		{Action: "Upgrade", Package: "symfony/process", From: "v7.0.9", To: "v7.1.0"},
		{Action: "Downgrade", Package: "justinrainbow/json-schema", From: "6.10.0", To: "6.8.2"},
	}, changes)
}

func TestUpdateDiffsAgainstTheLockAsItWasBeforeTheUpdate(t *testing.T) {
	dir := newUpdateProject(t)
	fake := &fakeUpdate{diffOut: `{"packages": [], "packages-dev": []}`}

	_, err := newUpdateCLI(fake.command(t)).Update(t.Context(), dir, nil, nil, false, false)
	require.NoError(t, err)

	assert.Equal(t, []string{baseLock}, fake.baseLocks)
	diff := fake.call(t, "diff")
	assert.Contains(t, diff, "--target=composer.lock")
	assert.Contains(t, diff, "--format=json")

	base, _ := strings.CutPrefix(diff[slices.Index(diff, "--target=composer.lock")-1], "--base=")
	assert.NoFileExists(t, base, "the base lock is a temporary file")
}

func TestUpdateKeepsTheUpdatedFiles(t *testing.T) {
	dir := newUpdateProject(t)
	fake := &fakeUpdate{diffOut: diffWithEveryOperation}

	_, err := newUpdateCLI(fake.command(t)).Update(t.Context(), dir, []string{"drupal/core"}, nil, false, false)
	require.NoError(t, err)

	assert.JSONEq(t, newJSON, readFile(t, dir, "composer.json"))
	assert.JSONEq(t, newLock, readFile(t, dir, "composer.lock"))

	update := fake.call(t, "update")
	assert.Contains(t, update, "drupal/core")
	assert.Contains(t, update, "--bump-after-update")
	assert.Contains(t, update, "--no-install", "vendor/ must keep the old code for the baseline install")
	assert.NotContains(t, update, "--no-scripts")
	assert.NotContains(t, update, "--dry-run")
	assert.NotContains(t, update, "--minimal-changes")
}

func TestUpdateDryRunPutsComposerFilesBack(t *testing.T) {
	dir := newUpdateProject(t)
	fake := &fakeUpdate{diffOut: diffWithEveryOperation}

	changes, err := newUpdateCLI(fake.command(t)).Update(t.Context(), dir, nil, []string{"drupal/foo:1.2.3"}, true, true)
	require.NoError(t, err)
	assert.Len(t, changes, 8, "a dry run still reports what the update would change")

	assert.JSONEq(t, baseJSON, readFile(t, dir, "composer.json"))
	assert.JSONEq(t, baseLock, readFile(t, dir, "composer.lock"))
	assert.Equal(t, []string{baseLock}, fake.baseLocks)

	update := fake.call(t, "update")
	assert.Contains(t, update, "--with=drupal/foo:1.2.3")
	assert.Contains(t, update, "--minimal-changes")
	assert.Contains(t, update, "--no-scripts")
	assert.Contains(t, update, "--no-install")
	assert.NotContains(t, update, "--bump-after-update")
	assert.NotContains(t, update, "--dry-run", "composer's own dry run writes no lock to diff")
}

func TestUpdateDryRunPutsComposerFilesBackWhenComposerFails(t *testing.T) {
	dir := newUpdateProject(t)
	fake := &fakeUpdate{diffFails: true}

	_, err := newUpdateCLI(fake.command(t)).Update(t.Context(), dir, nil, nil, false, true)
	require.Error(t, err)

	assert.JSONEq(t, baseJSON, readFile(t, dir, "composer.json"))
	assert.JSONEq(t, baseLock, readFile(t, dir, "composer.lock"))
}

func TestUpdatePropagatesFailure(t *testing.T) {
	dir := newUpdateProject(t)
	fake := &fakeUpdate{updateFails: true}

	changes, err := newUpdateCLI(fake.command(t)).Update(t.Context(), dir, nil, nil, false, false)
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr)
	assert.Empty(t, changes)
	assert.Contains(t, err.Error(), "failed to update dependencies")
	assert.Contains(t, err.Error(), "could not be resolved", "composer's own explanation is kept")
}

func TestUpdatePropagatesDiffFailure(t *testing.T) {
	dir := newUpdateProject(t)
	fake := &fakeUpdate{diffFails: true}

	_, err := newUpdateCLI(fake.command(t)).Update(t.Context(), dir, nil, nil, false, false)
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr)
	assert.Contains(t, err.Error(), "failed to diff composer.lock")
}

func TestUpdateRejectsMalformedDiffOutput(t *testing.T) {
	dir := newUpdateProject(t)
	fake := &fakeUpdate{diffOut: "not json"}

	_, err := newUpdateCLI(fake.command(t)).Update(t.Context(), dir, nil, nil, false, false)
	require.Error(t, err)
}

func TestUpdateRejectsAnUnknownDiffOperation(t *testing.T) {
	dir := newUpdateProject(t)
	fake := &fakeUpdate{diffOut: `{"packages": {"a/b": {"name": "a/b", "operation": "teleport", "version_base": "1.0.0", "version_target": "2.0.0"}}}`}

	_, err := newUpdateCLI(fake.command(t)).Update(t.Context(), dir, nil, nil, false, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown composer-diff operation "teleport" for a/b`)
}

func TestUpdateReportsNoChangesAsNil(t *testing.T) {
	dir := newUpdateProject(t)
	fake := &fakeUpdate{diffOut: `{"packages": [], "packages-dev": []}`}

	changes, err := newUpdateCLI(fake.command(t)).Update(t.Context(), dir, nil, nil, false, false)
	require.NoError(t, err)
	assert.Nil(t, changes)
}

func TestUpdateWithoutALockFileNeverRunsComposer(t *testing.T) {
	fake := &fakeUpdate{}

	_, err := newUpdateCLI(fake.command(t)).Update(t.Context(), t.TempDir(), nil, nil, false, false)
	require.ErrorIs(t, err, os.ErrNotExist)
	assert.Contains(t, err.Error(), "failed to read composer.lock")
	assert.Empty(t, fake.calls)
}

func TestUpdateDryRunWithoutComposerJSONNeverRunsComposer(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "composer.lock"), []byte(baseLock), 0o600))
	fake := &fakeUpdate{}

	_, err := newUpdateCLI(fake.command(t)).Update(t.Context(), dir, nil, nil, false, true)
	require.ErrorIs(t, err, os.ErrNotExist)
	assert.Contains(t, err.Error(), "failed to read composer.json")
	assert.Empty(t, fake.calls)
}

func TestUpdatePinsADevPackageByCommit(t *testing.T) {
	dir := newUpdateProject(t)
	fake := &fakeUpdate{diffOut: `{"packages": []}`}

	_, err := newUpdateCLI(fake.command(t)).Update(t.Context(), dir, nil, []string{"symfony/polyfill-ctype:1.x-dev 0424dff"}, false, true)
	require.NoError(t, err)

	assert.Contains(t, fake.call(t, "update"), "--with=symfony/polyfill-ctype:1.x-dev#0424dff")
}

func TestConstraint(t *testing.T) {
	assert.Equal(t, "1.2.3", constraint("1.2.3"))
	assert.Equal(t, "v7.4.14", constraint("v7.4.14"))
	assert.Equal(t, "1.x-dev#0424dff", constraint("1.x-dev 0424dff"))
	assert.Equal(t, "dev-main#141046a", constraint("dev-main 141046a"))
}

func TestRequireIntoScratchPinsADevPackageByCommit(t *testing.T) {
	fake := &fakeUpdate{}

	service := &CLI{newCommand: fake.command(t), logger: zap.NewNop(), tempDir: t.TempDir()}
	ok, err := service.requireIntoScratch(t.Context(), "drupal/foo", "1.x-dev 0424dff")
	require.NoError(t, err)
	assert.True(t, ok)

	assert.Contains(t, fake.call(t, "require"), "drupal/foo:1.x-dev#0424dff")
}

func TestUpdateDryRunReportsAFailedRestore(t *testing.T) {
	dir := newUpdateProject(t)
	fake := &fakeUpdate{diffOut: `{"packages": []}`}

	fs := &faultFs{Fs: afero.NewOsFs(), openErr: func(name string, flag int) error {
		if flag&os.O_WRONLY != 0 && strings.HasPrefix(filepath.Base(name), "composer.") {
			return os.ErrPermission
		}
		return nil
	}}
	_, err := (&CLI{newCommand: fake.command(t), logger: zap.NewNop(), fs: fs}).Update(t.Context(), dir, nil, nil, false, true)
	require.ErrorIs(t, err, os.ErrPermission)
	assert.Contains(t, err.Error(), "failed to restore composer.")
}

func TestUpdateFailsWhenTheBaseLockCannotBeCreated(t *testing.T) {
	dir := newUpdateProject(t)
	fake := &fakeUpdate{diffOut: `{"packages": []}`}

	fs := &faultFs{Fs: afero.NewOsFs(), openErr: func(name string, _ int) error {
		if strings.HasPrefix(filepath.Base(name), "drupdater-base-") {
			return os.ErrPermission
		}
		return nil
	}}
	_, err := (&CLI{newCommand: fake.command(t), logger: zap.NewNop(), fs: fs}).Update(t.Context(), dir, nil, nil, false, false)
	require.ErrorIs(t, err, os.ErrPermission)
	assert.Contains(t, err.Error(), "failed to create the base lock file")
	assert.Len(t, fake.calls, 1, "composer diff never runs without a base")
}

func TestUpdateFailsWhenTheBaseLockCannotBeWritten(t *testing.T) {
	dir := newUpdateProject(t)
	fake := &fakeUpdate{diffOut: `{"packages": []}`}

	fs := &faultFs{Fs: afero.NewOsFs(), writeErr: os.ErrClosed}
	_, err := (&CLI{newCommand: fake.command(t), logger: zap.NewNop(), fs: fs}).Update(t.Context(), dir, nil, nil, false, false)
	require.ErrorIs(t, err, os.ErrClosed)
	assert.Contains(t, err.Error(), "failed to write the base lock file")
	assert.Len(t, fake.calls, 1, "composer diff never runs against a partial base")
}

func TestUpdateWarnsOnlyWhenTheBaseLockCannotBeRemoved(t *testing.T) {
	for _, tc := range []struct {
		name      string
		removeErr error
		warnings  int
	}{
		{name: "removed", warnings: 0},
		{name: "left behind", removeErr: os.ErrPermission, warnings: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := newUpdateProject(t)
			fake := &fakeUpdate{diffOut: `{"packages": []}`}

			core, logs := observer.New(zap.WarnLevel)
			fs := &faultFs{Fs: afero.NewOsFs(), removeErr: tc.removeErr}
			_, err := (&CLI{newCommand: fake.command(t), logger: zap.New(core), fs: fs}).Update(t.Context(), dir, nil, nil, false, false)
			require.NoError(t, err, "a leftover temporary file does not fail the update")
			assert.Equal(t, tc.warnings, logs.FilterMessage("failed to remove the base lock file").Len())
		})
	}
}

// faultFs fails the operations a test names and passes everything else to Fs.
type faultFs struct {
	afero.Fs
	openErr   func(name string, flag int) error
	writeErr  error
	removeErr error
}

func (f *faultFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if f.openErr != nil {
		if err := f.openErr(name, flag); err != nil {
			return nil, err
		}
	}
	file, err := f.Fs.OpenFile(name, flag, perm)
	if err != nil || f.writeErr == nil {
		return file, err
	}
	return &failingWriteFile{File: file, err: f.writeErr}, nil
}

func (f *faultFs) Remove(name string) error {
	if f.removeErr != nil {
		_ = f.Fs.Remove(name)
		return f.removeErr
	}
	return f.Fs.Remove(name)
}

type failingWriteFile struct {
	afero.File
	err error
}

func (f *failingWriteFile) Write([]byte) (int, error) { return 0, f.err }
