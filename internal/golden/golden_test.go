package golden

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubTB records what Assert reported instead of failing the real test, so both failure paths
// can be checked. FailNow only marks: a require inside Assert would otherwise abort this test.
type stubTB struct {
	errors []string
	failed bool
}

func (s *stubTB) Helper()             {}
func (s *stubTB) Logf(string, ...any) {}
func (s *stubTB) Errorf(format string, args ...any) {
	s.errors = append(s.errors, fmt.Sprintf(format, args...))
}
func (s *stubTB) FailNow() { s.failed = true }

// withUpdate flips the -update flag for one test and restores it, so the package's own tests do
// not depend on how the suite was invoked.
func withUpdate(t *testing.T, on bool) {
	t.Helper()
	previous := *update
	*update = on
	t.Cleanup(func() { *update = previous })
}

func TestAssertPassesWhenTheFileMatches(t *testing.T) {
	withUpdate(t, false)
	path := filepath.Join(t.TempDir(), "golden.txt")
	require.NoError(t, os.WriteFile(path, []byte("hello\nworld\n"), 0o600))

	stub := &stubTB{}
	Assert(stub, path, "hello\nworld\n")

	assert.Empty(t, stub.errors)
}

func TestAssertReportsAMismatch(t *testing.T) {
	withUpdate(t, false)
	path := filepath.Join(t.TempDir(), "golden.txt")
	require.NoError(t, os.WriteFile(path, []byte("hello\nworld\n"), 0o600))

	stub := &stubTB{}
	Assert(stub, path, "hello\nthere\n")

	assert.NotEmpty(t, stub.errors, "a differing file has to fail the test")
}

func TestAssertReportsAMissingFile(t *testing.T) {
	withUpdate(t, false)

	stub := &stubTB{}
	Assert(stub, filepath.Join(t.TempDir(), "absent.txt"), "anything")

	// Not merely reported: without FailNow the comparison would run on empty content and
	// report a confusing diff instead of "there is no golden file".
	assert.True(t, stub.failed)
	assert.NotEmpty(t, stub.errors)
}

func TestUpdateWritesTheFileAndItsDirectory(t *testing.T) {
	withUpdate(t, true)
	path := filepath.Join(t.TempDir(), "nested", "golden.txt")

	stub := &stubTB{}
	Assert(stub, path, "generated\n")

	require.Empty(t, stub.errors)
	written, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "generated\n", string(written))
}

func TestUpdateOverwritesAndThenCompares(t *testing.T) {
	path := filepath.Join(t.TempDir(), "golden.txt")
	require.NoError(t, os.WriteFile(path, []byte("stale\n"), 0o600))

	withUpdate(t, true)
	Assert(&stubTB{}, path, "fresh\n")

	// The round trip is the contract: whatever -update wrote must then compare clean, or
	// regenerating would leave the suite red.
	withUpdate(t, false)
	stub := &stubTB{}
	Assert(stub, path, "fresh\n")
	assert.Empty(t, stub.errors)
}
