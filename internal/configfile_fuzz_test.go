package internal

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// .drupdater.yaml is committed by the project and read before anything else happens, so a broken
// one has to be rejected whole. The invariant worth fuzzing is not that the parser accepts the
// right files but that a rejected file leaves the Config the run proceeds with untouched.
func FuzzLoadConfigFile(f *testing.F) {
	for _, seed := range []string{
		"sites: [default]\ntimeout: 30m\n",
		"timeout: 0\n",
		"run_types:\n  security:\n    addons: []\n    auto_merge: true\n",
		"addons:\n  normal: []\n",
		"sites: []\n",
		"sites:\n  - default\n  - second\n",
		"# comment only\n",
		"",
		"\x00",
		"timeout: [not, a, duration]\n",
		"unknown_key: 1\n",
	} {
		f.Add(seed)
	}

	// One directory per worker, rewritten per input, rather than a t.TempDir() inside the target
	// creating and removing one on every execution. Workers are separate processes running one
	// input at a time, so sharing the path is safe.
	path := filepath.Join(f.TempDir(), ".drupdater.yaml")

	f.Fuzz(func(t *testing.T, body string) {
		require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

		before := Config{Sites: []string{"sentinel"}, Timeout: time.Hour}
		c := before
		found, err := LoadConfigFile(path, &c)

		assert.True(t, found, "the file exists, so it was found whatever it contains")
		if err != nil {
			assert.Equal(t, before, c, "a rejected config must not apply half of itself")
			return
		}

		// An empty list would silently skip every per-site phase and then open the merge
		// request anyway, which reads as a successful run that did nothing.
		assert.NotEmpty(t, c.Sites, "an accepted config always names at least one site")
	})
}
