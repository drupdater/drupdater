package composer

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

// The helpers below read output produced outside this repository's control, so these properties
// state each one's promise over the whole input space rather than over one example.

// jsonKeyGen generates an object key of the shape composer.json uses for a repository entry.
func jsonKeyGen() *rapid.Generator[string] {
	return rapid.StringMatching(`[a-z][a-z0-9_.-]{0,10}`)
}

func TestPropertyOrderedObjectValuesKeepsDeclarationOrder(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		keys := rapid.SliceOfN(jsonKeyGen(), 0, 6).Draw(t, "keys")

		// Indexed so a reordering is visible: sorting the keys, which this once did, shuffles
		// the repositories into a priority the project never declared.
		pairs := make([]string, 0, len(keys))
		want := make([]json.RawMessage, 0, len(keys))
		for i, key := range keys {
			value := fmt.Sprintf(`{"pos":%d}`, i)
			pairs = append(pairs, fmt.Sprintf("%q:%s", key, value))
			want = append(want, json.RawMessage(value))
		}

		got, ok := orderedObjectValues(json.RawMessage("{" + strings.Join(pairs, ",") + "}"))
		require.True(t, ok)
		assert.Len(t, got, len(keys), "duplicate keys count too — the object is not a map here")
		for i := range want {
			assert.JSONEq(t, string(want[i]), string(got[i]), "value %d", i)
		}
	})
}

func TestPropertyOrderedObjectValuesRejectsEverythingButObjects(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		raw := rapid.OneOf(
			rapid.String(),
			rapid.StringMatching(`\[(1(,1)*)?\]`),
			rapid.StringMatching(`(-?[0-9]{1,4}|true|false|null|"[a-z]{0,5}")`),
		).Filter(func(s string) bool {
			// An object is the one input this property is not about, and a random string can
			// happen to open one.
			return !strings.HasPrefix(strings.TrimSpace(s), "{")
		}).Draw(t, "raw")

		// composer.json is user-supplied, so a "repositories" key holding an array or a scalar
		// has to be reported as not-an-object rather than crash the run.
		values, ok := orderedObjectValues(json.RawMessage(raw))
		assert.False(t, ok, "for %q", raw)
		assert.Nil(t, values)
	})
}

func TestPropertyNormalizeRepositoryDropsOnlyTheDisableForm(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		name := jsonKeyGen().Draw(t, "name")
		disabled := rapid.Bool().Draw(t, "disabled")

		// Matched on the exact disable shape, because a real entry carries a bool of its own:
		// a mirror is {"type":"composer","url":"…","canonical":false}.
		_, keep := normalizeRepository(json.RawMessage(fmt.Sprintf(`{%q:%t}`, name, disabled)), "/project")
		assert.False(t, keep, "a single name mapped to a bool is a disable entry")

		mirror := json.RawMessage(fmt.Sprintf(`{"type":"composer","url":"https://example.com","canonical":%t}`, disabled))
		got, keep := normalizeRepository(mirror, "/project")
		assert.True(t, keep, "an entry with more than one key is a repository, bool or not")
		assert.JSONEq(t, string(mirror), string(got))
	})
}

func TestPropertyNormalizeRepositoryResolvesRelativePathsOnce(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		projectDir := "/" + strings.Join(rapid.SliceOfN(jsonKeyGen(), 1, 3).Draw(t, "projectDir"), "/")
		relative := strings.Join(rapid.SliceOfN(jsonKeyGen(), 1, 3).Draw(t, "relative"), "/")

		entry := json.RawMessage(fmt.Sprintf(`{"type":"path","url":%q}`, relative))
		normalized, keep := normalizeRepository(entry, projectDir)
		require.True(t, keep)

		var got struct {
			URL string `json:"url"`
		}
		require.NoError(t, json.Unmarshal(normalized, &got))
		assert.Equal(t, filepath.Join(projectDir, relative), got.URL)

		// Idempotent: the resolved URL is absolute, and a doubled prefix points nowhere.
		again, keep := normalizeRepository(normalized, projectDir)
		require.True(t, keep)
		assert.JSONEq(t, string(normalized), string(again))
	})
}

func TestPropertyNormalizeRepositoryPassesNonPathEntriesThrough(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		repoType := rapid.SampledFrom([]string{"composer", "vcs", "git", "artifact", "package"}).Draw(t, "type")
		url := rapid.StringMatching(`https://[a-z]{1,8}\.example\.com/[a-z]{0,8}`).Draw(t, "url")

		// Only "path" entries are rewritten. Everything else must arrive byte-identical:
		// re-marshalling reorders keys and can drop a field this struct does not know.
		entry := json.RawMessage(fmt.Sprintf(`{"type":%q,"url":%q}`, repoType, url))
		got, keep := normalizeRepository(entry, "/project")
		assert.True(t, keep)
		assert.Equal(t, string(entry), string(got))
	})
}

func TestPropertyUnresolvableReasonLetsPatchRejectionWin(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		rejection := rapid.SampledFrom([]string{"could not apply patch", "cannot apply patch"}).Draw(t, "rejection")
		markers := rapid.SliceOfN(rapid.SampledFrom([]string{
			"could not find package",
			"could not find a matching version",
			"could not be found",
			"invalid credentials",
			"authentication required",
			"could not be downloaded",
		}), 0, 4).Draw(t, "markers")
		noise := rapid.StringMatching(`[a-z .:/'"-]{0,40}`).Draw(t, "noise")

		// The dist-to-source fallback prints "could not be downloaded" and carries on, so an
		// unresolvable marker can sit in output describing a genuine rejection. Shipping a
		// patch that does not apply is the worse failure, so the rejection always wins.
		out := noise + rejection + noise + strings.Join(markers, noise)
		reason, unresolvable := unresolvableReason(out)
		assert.False(t, unresolvable)
		assert.Empty(t, reason)
	})
}

func TestPropertyUnresolvableReasonIgnoresCase(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		out := rapid.SampledFrom([]string{
			"Could not find package foo/bar in any version",
			"The \"https://example.com/x.zip\" file could not be downloaded",
			"Invalid credentials for https://repo.example.com",
			"Authentication required (repo.example.com)",
			"Could not apply patch! Skipping.",
			"nothing interesting happened here",
		}).Draw(t, "out")
		upper := rapid.SliceOfN(rapid.Bool(), len(out), len(out)).Draw(t, "upper")

		var flipped strings.Builder
		for i, c := range []byte(out) {
			if upper[i] {
				flipped.WriteString(strings.ToUpper(string(c)))
				continue
			}
			flipped.WriteString(strings.ToLower(string(c)))
		}

		// Composer's capitalisation varies with the message and its version; the classification
		// must not.
		wantReason, wantUnresolvable := unresolvableReason(out)
		gotReason, gotUnresolvable := unresolvableReason(flipped.String())
		assert.Equal(t, wantUnresolvable, gotUnresolvable)
		assert.Equal(t, wantReason, gotReason)
	})
}

func TestPropertyUnresolvableReasonIsNotUndoneByTrailingOutput(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		marker := rapid.SampledFrom([]string{
			"could not find package",
			"could not find a matching version",
			"could not be found",
			"invalid credentials",
			"authentication required",
			"could not be downloaded",
		}).Draw(t, "marker")
		// Free of both the patch-rejection wording and any other marker, so appending it cannot
		// legitimately change the answer.
		extra := rapid.StringMatching(`[a-z0-9 .:/-]{0,30}`).
			Filter(func(s string) bool { return !strings.Contains(s, "could") && !strings.Contains(s, "cannot") }).
			Draw(t, "extra")

		reason, unresolvable := unresolvableReason(extra + marker + extra)
		assert.True(t, unresolvable)
		assert.NotEmpty(t, reason, "an unresolvable package has to come with a reason for the report")
	})
}

func TestPropertyAuditUnmarshalFlattensEveryShape(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		packages := rapid.SliceOfNDistinct(jsonKeyGen(), 0, 4, rapid.ID).Draw(t, "packages")

		// Both shapes must flatten into one list, or an advisory disappears from the report.
		entries := make([]string, 0, len(packages))
		want := make([]string, 0)
		for _, pkg := range packages {
			ids := rapid.SliceOfNDistinct(jsonKeyGen(), 0, 3, rapid.ID).Draw(t, "ids-"+pkg)
			advisories := make([]string, 0, len(ids))
			for _, id := range ids {
				advisories = append(advisories, fmt.Sprintf(`{"advisoryId":%q}`, id))
				want = append(want, id)
			}

			if rapid.Bool().Draw(t, "asMap-"+pkg) {
				keyed := make([]string, 0, len(advisories))
				for i, advisory := range advisories {
					keyed = append(keyed, fmt.Sprintf("%q:%s", fmt.Sprintf("k%d", i), advisory))
				}
				entries = append(entries, fmt.Sprintf("%q:{%s}", pkg, strings.Join(keyed, ",")))
				continue
			}
			entries = append(entries, fmt.Sprintf("%q:[%s]", pkg, strings.Join(advisories, ",")))
		}

		var audit Audit
		require.NoError(t, json.Unmarshal([]byte(fmt.Sprintf(`{"advisories":{%s}}`, strings.Join(entries, ","))), &audit))

		// Compared as a set: this property is about losing none of them, not about their
		// order — TestAuditUnmarshalShapes and FuzzAuditUnmarshalJSON cover the sort.
		got := make([]string, 0, len(audit.Advisories))
		for _, advisory := range audit.Advisories {
			got = append(got, advisory.AdvisoryID)
		}
		assert.ElementsMatch(t, want, got)
	})
}

func TestPropertyAuditUnmarshalToleratesAMissingAdvisoriesKey(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		keys := rapid.SliceOfNDistinct(jsonKeyGen().Filter(func(s string) bool { return s != "advisories" }), 0, 4, rapid.ID).Draw(t, "keys")

		pairs := make([]string, 0, len(keys))
		for _, key := range keys {
			pairs = append(pairs, fmt.Sprintf(`%q:1`, key))
		}

		// `composer audit` reports no advisories by omitting the key, which is the common case
		// and must not read as a failure to parse.
		var audit Audit
		require.NoError(t, json.Unmarshal([]byte("{"+strings.Join(pairs, ",")+"}"), &audit))
		assert.Empty(t, audit.Advisories)
	})
}

// The abandoned list is built by ranging a Go map, so without the sort the same audit output
// renders a different report every run and a consumer diffing two sees phantom changes.
func TestPropertyAuditUnmarshalOrdersAbandonedByName(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		names := rapid.SliceOfNDistinct(jsonKeyGen(), 0, 6, rapid.ID).Draw(t, "names")

		entries := make([]string, 0, len(names))
		want := make([]AbandonedPackage, 0, len(names))
		for _, name := range names {
			// Composer writes null when the maintainers suggested no successor.
			if rapid.Bool().Draw(t, "hasReplacement-"+name) {
				replacement := jsonKeyGen().Draw(t, "replacement-"+name)
				entries = append(entries, fmt.Sprintf("%q:%q", name, replacement))
				want = append(want, AbandonedPackage{PackageName: name, Replacement: replacement})
				continue
			}
			entries = append(entries, fmt.Sprintf("%q:null", name))
			want = append(want, AbandonedPackage{PackageName: name})
		}
		slices.SortFunc(want, func(a, b AbandonedPackage) int {
			return strings.Compare(a.PackageName, b.PackageName)
		})

		var audit Audit
		require.NoError(t, json.Unmarshal([]byte(fmt.Sprintf(`{"abandoned":{%s}}`, strings.Join(entries, ","))), &audit))

		// Equal, not ElementsMatch: the order is the property under test.
		assert.Equal(t, want, audit.Abandoned)
	})
}

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
// example-based tests in composer_env_test.go.
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
