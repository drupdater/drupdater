package addon

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// declaredFrom orders a map literal's packages and descriptions by name, so a test is repeatable.
func declaredFrom(m map[string]map[string]string) declaredPatches {
	d := declaredPatches{sets: map[string][]patchEntry{}}
	for _, pkg := range sortedKeys(m) {
		d.packages = append(d.packages, pkg)
		d.sets[pkg] = []patchEntry{}
		for _, description := range sortedKeys(m[pkg]) {
			d.sets[pkg] = append(d.sets[pkg], patchEntry{description: description, path: m[pkg][description]})
		}
	}
	return d
}

func asMap(d declaredPatches) map[string]map[string]string {
	m := map[string]map[string]string{}
	for _, pkg := range d.packages {
		m[pkg] = map[string]string{}
		for _, e := range d.sets[pkg] {
			m[pkg][e.description] = e.path
		}
	}
	return m
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func ptr[T any](v T) *T { return &v }

const unsortedPatches = `{"drupal/token":{"z first":"patches/z.patch","a second":"https://example.com/a.patch"},` +
	`"drupal/core":{"only":"patches/core.patch"},"drupal/empty":{}}`

func TestDeclaredPatchesKeepTheirDeclarationOrder(t *testing.T) {
	var d declaredPatches
	require.NoError(t, json.Unmarshal([]byte(unsortedPatches), &d))

	assert.Equal(t, []string{"drupal/token", "drupal/core", "drupal/empty"}, d.packages)
	assert.Equal(t, []patchEntry{{"z first", "patches/z.patch"}, {"a second", "https://example.com/a.patch"}}, d.sets["drupal/token"])
	assert.Equal(t, []patchEntry{}, d.sets["drupal/empty"])

	encoded, err := json.Marshal(d)
	require.NoError(t, err)
	assert.Equal(t, unsortedPatches, string(encoded), "written back byte for byte, order and empty sets included") //nolint:testifylint // JSONEq ignores key order
}

func TestDeclaredPatchesReadNullAsNone(t *testing.T) {
	var d declaredPatches
	require.NoError(t, json.Unmarshal([]byte(" null "), &d))

	encoded, err := json.Marshal(d)
	require.NoError(t, err)
	assert.Equal(t, "{}", string(encoded))
}

func TestDeclaredPatchesKeepARepeatedDescriptionInItsFirstPlaceWithItsLastPath(t *testing.T) {
	var d declaredPatches
	require.NoError(t, json.Unmarshal([]byte(`{"p":{"a":"1","b":"2","a":"3"},"q":{"x":"y"},"p":{"c":"4"}}`), &d))

	assert.Equal(t, []string{"p", "q"}, d.packages)
	assert.Equal(t, []patchEntry{{"c", "4"}}, d.sets["p"], "a repeated package key replaces the earlier one, as in PHP")

	require.NoError(t, json.Unmarshal([]byte(`{"p":{"a":"1","b":"2","a":"3"}}`), &d))
	assert.Equal(t, []patchEntry{{"a", "3"}, {"b", "2"}}, d.sets["p"])
}

func TestDeclaredPatchesEscapeWhatTheyWrite(t *testing.T) {
	d := declaredFrom(map[string]map[string]string{"drupal/core": {`Issue #1: ["quoted" \ ü](https://x/?a=1&b=2)`: "patches/a b.patch"}})

	encoded, err := json.Marshal(d)
	require.NoError(t, err)
	var back declaredPatches
	require.NoError(t, json.Unmarshal(encoded, &back))
	assert.Equal(t, d, back)
}

func TestDeclaredPatchesRefuseWhatIsNotAPatchObject(t *testing.T) {
	for name, raw := range map[string]string{
		"not JSON":            `not-json`,
		"a list":              `["patches/a.patch"]`,
		"a package as a list": `{"drupal/core":["patches/a.patch"]}`,
		"a path not a string": `{"drupal/core":{"a":1}}`,
		"truncated":           `{"drupal/core":{"a":"b"}`,
		"a number key":        `{"drupal/core":{"a":"b"},`,
	} {
		t.Run(name, func(t *testing.T) {
			var d declaredPatches
			require.Error(t, json.Unmarshal([]byte(raw), &d))
		})
	}
}

func TestDeclaredPatchesRenameInPlace(t *testing.T) {
	d := declaredFrom(map[string]map[string]string{"p": {"a": "1", "b": "2", "c": "3"}})

	assert.True(t, d.rename("p", "b", "renamed"))
	assert.Equal(t, []patchEntry{{"a", "1"}, {"renamed", "2"}, {"c", "3"}}, d.sets["p"])

	assert.False(t, d.rename("p", "a", "c"), "two patches may not share a description")
	assert.False(t, d.rename("p", "missing", "new"))
	assert.False(t, d.rename("other", "a", "new"))
	assert.Equal(t, []patchEntry{{"a", "1"}, {"renamed", "2"}, {"c", "3"}}, d.sets["p"])
}

func TestDeclaredPatchesSetPathAndRemove(t *testing.T) {
	d := declaredFrom(map[string]map[string]string{"p": {"a": "1", "b": "2"}, "q": {"x": "y"}})

	d.setPath("p", "b", "new")
	assert.Equal(t, []patchEntry{{"a", "1"}, {"b", "new"}}, d.sets["p"])

	d.remove("p", "a")
	assert.Equal(t, []patchEntry{{"b", "new"}}, d.sets["p"])
	d.remove("p", "b")
	assert.Equal(t, []string{"q"}, d.packages, "a package with no patches left goes")
	assert.NotContains(t, d.sets, "p")
}
