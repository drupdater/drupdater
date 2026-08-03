package report

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// failingFs fails one chosen operation and delegates the rest, making writeJSON's atomicity
// branches reachable — on a real filesystem they need a full disk or a mid-write chmod.
type failingFs struct {
	afero.Fs
	mkdirAll bool
	write    bool
	close    bool
	rename   bool
}

func (f *failingFs) MkdirAll(path string, perm os.FileMode) error {
	if f.mkdirAll {
		return assert.AnError
	}
	return f.Fs.MkdirAll(path, perm)
}

func (f *failingFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	file, err := f.Fs.OpenFile(name, flag, perm)
	if err != nil || (!f.write && !f.close) {
		return file, err
	}
	return &failingFile{File: file, write: f.write, close: f.close}, nil
}

func (f *failingFs) Rename(oldname, newname string) error {
	if f.rename {
		return assert.AnError
	}
	return f.Fs.Rename(oldname, newname)
}

type failingFile struct {
	afero.File
	write bool
	close bool
}

func (f *failingFile) Write(p []byte) (int, error) {
	if f.write {
		return 0, assert.AnError
	}
	return f.File.Write(p)
}

func (f *failingFile) Close() error {
	err := f.File.Close()
	if f.close {
		return assert.AnError
	}
	return err
}

// tempFilesIn counts the leftover temp files writeJSON creates, so each failure path can be
// checked for cleaning up after itself rather than littering the report directory.
func tempFilesIn(t *testing.T, fs afero.Fs, dir string) int {
	t.Helper()
	entries, err := afero.ReadDir(fs, dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if len(e.Name()) > 0 && e.Name()[0] == '.' {
			n++
		}
	}
	return n
}

func TestWriteReportFailurePaths(t *testing.T) {
	tests := []struct {
		name    string
		fs      func(afero.Fs) afero.Fs
		wantMsg string
	}{
		{
			name:    "report directory cannot be created",
			fs:      func(base afero.Fs) afero.Fs { return &failingFs{Fs: base, mkdirAll: true} },
			wantMsg: "failed to create report directory",
		},
		{
			name:    "temp file cannot be written",
			fs:      func(base afero.Fs) afero.Fs { return &failingFs{Fs: base, write: true} },
			wantMsg: "failed to write report",
		},
		{
			name:    "temp file cannot be closed",
			fs:      func(base afero.Fs) afero.Fs { return &failingFs{Fs: base, close: true} },
			wantMsg: "failed to close report",
		},
		{
			name:    "temp file cannot be renamed into place",
			fs:      func(base afero.Fs) afero.Fs { return &failingFs{Fs: base, rename: true} },
			wantMsg: "failed to move report into place",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := afero.NewMemMapFs()
			dir := "/out"
			require.NoError(t, base.MkdirAll(dir, 0o755))
			path := filepath.Join(dir, "report.json")

			err := Write(tt.fs(base), path, Report{SchemaVersion: 1}, nil)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantMsg)
			// The cause has to survive the wrapper: a caller distinguishing a full disk from a
			// permission problem cannot do it by message text.
			require.ErrorIs(t, err, assert.AnError)

			// Nothing half-written is left behind, under any failure.
			exists, _ := afero.Exists(base, path)
			assert.False(t, exists, "the report must not exist after a failed write")
			assert.Zero(t, tempFilesIn(t, base, dir), "the temp file must be cleaned up")
		})
	}
}

func TestWriteReportIsAtomic(t *testing.T) {
	fs := afero.NewMemMapFs()
	path := "/out/nested/report.json"

	// The directory does not exist yet: writeJSON creates it rather than failing.
	require.NoError(t, Write(fs, path, Report{SchemaVersion: 1}, nil))

	exists, err := afero.Exists(fs, path)
	require.NoError(t, err)
	assert.True(t, exists)
	assert.Zero(t, tempFilesIn(t, fs, "/out/nested"), "no temp file may survive a successful write")

	content, err := afero.ReadFile(fs, path)
	require.NoError(t, err)
	// Trailing newline: the report is meant to be readable with ordinary line-based tooling.
	assert.Equal(t, byte('\n'), content[len(content)-1])
}

func TestSanitizeURLNormalizesOnlyWhenItReparses(t *testing.T) {
	// Both halves of the guard matter: a URL with no credentials is returned verbatim, not
	// round-tripped through url.String(), which lower-cases the scheme and host.
	const mixedCase = "HTTPS://Example.COM/Group/Repo.git"
	assert.Equal(t, mixedCase, SanitizeURL(mixedCase))

	// With credentials it is reparsed, and only the userinfo is dropped.
	assert.Equal(t, "https://example.com/group/repo.git",
		SanitizeURL("https://user:token@example.com/group/repo.git"))
}
