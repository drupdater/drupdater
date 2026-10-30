package php

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"pgregory.net/rapid"
)

// Env forces the entries drupdater's correctness depends on and passes everything else through
// unchanged and in order — rebuilding an environment silently drops variables nobody tested.
func TestPropertyComposerEnvLeavesEverythingElseAlone(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		env := rapid.SliceOfN(envEntryGen(), 0, 8).Draw(t, "env")

		got := Env(env)

		// Every entry that does not assign a forced variable survives, in its original order.
		var want []string
		for _, entry := range env {
			if !assignsRequiredKey(entry) {
				want = append(want, entry)
			}
		}
		assert.Equal(t, want, withoutRequired(got))

		// And each forced variable is assigned exactly once, to the value drupdater needs.
		for _, required := range requiredComposerEnv {
			key, value, _ := strings.Cut(required, "=")
			assert.Equal(t, []string{value}, valuesOf(got, key))
		}

		// Applying it again changes nothing: the result is already a valid composer environment.
		assert.Equal(t, got, Env(got))
	})
}

// envEntryGen draws forced keys often enough to exercise the override path, and sometimes emits
// a non-assignment: os.Environ() does not promise every entry contains "=".
func envEntryGen() *rapid.Generator[string] {
	return rapid.Custom(func(t *rapid.T) string {
		key := rapid.SampledFrom([]string{
			"COMPOSER_PROCESS_TIMEOUT", "COMPOSER_NO_AUDIT", "COMPOSER_AUTH",
			"COMPOSER_HOME", "COMPOSER_CACHE_DIR", "PATH", "",
		}).Draw(t, "key")
		if key == "" {
			return rapid.StringMatching(`[A-Z_]{1,8}`).Draw(t, "bare")
		}
		return key + "=" + rapid.StringMatching(`[a-z0-9/{}:_-]{0,10}`).Draw(t, "value")
	})
}

func assignsRequiredKey(entry string) bool {
	key, _, ok := strings.Cut(entry, "=")
	if !ok {
		return false
	}
	return slices.ContainsFunc(requiredComposerEnv, func(required string) bool {
		requiredKey, _, _ := strings.Cut(required, "=")
		return requiredKey == key
	})
}

// withoutRequired drops the entries Env appends, leaving what it carried over. Shared with the
// example-based tests in command_test.go.
func withoutRequired(env []string) []string {
	var result []string
	for _, entry := range env {
		if !slices.Contains(requiredComposerEnv, entry) {
			result = append(result, entry)
		}
	}
	return result
}

// valuesOf returns the values every entry of env assigns to key, in order.
func valuesOf(env []string, key string) []string {
	var values []string
	for _, entry := range env {
		if entryKey, value, ok := strings.Cut(entry, "="); ok && entryKey == key {
			values = append(values, value)
		}
	}
	return values
}
