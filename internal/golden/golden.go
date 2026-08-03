// Package golden compares test output against a committed file, and rewrites those files when
// the suite runs with -update. Imported only from _test.go files, so the flag it registers never
// reaches the drupdater binary.
//
// Golden files earn their keep on published output — the merge request sections and the --report
// document — where the whole shape is the contract and a field rename has to show up as a diff
// somebody reviews. They are the wrong tool for an internal value, where they freeze incidental
// detail and churn on unrelated changes.
package golden

import (
	"flag"
	"os"
	"path/filepath"
	"strings"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var update = flag.Bool("update", false, "rewrite golden files instead of comparing against them")

// TB is the part of testing.TB this package needs. Narrower than testing.TB, which cannot be
// implemented outside the standard library, so the failure paths here are themselves testable.
type TB interface {
	Helper()
	Logf(format string, args ...any)
	Errorf(format string, args ...any)
	FailNow()
}

// Assert compares actual against the file at path, or writes actual there when -update is set.
func Assert(t TB, path string, actual string) {
	t.Helper()

	if *update {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(actual), 0o600))
		t.Logf("updated %s", path)
		return
	}

	want, err := os.ReadFile(path) // #nosec G304 -- test-only, path comes from the test itself
	require.NoError(t, err, "missing golden file %s — regenerate with: go test ./... -update", path)
	if string(want) == actual {
		return
	}

	// Compared line by line rather than as one string: testify prints both sides in full, and
	// for a golden of any size the diff is the only readable part of that.
	assert.Equal(t, strings.Split(string(want), "\n"), strings.Split(actual, "\n"),
		"%s is out of date — inspect the diff, then regenerate with: go test ./... -update", path)
}
