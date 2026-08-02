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

// The helpers below all read output or configuration produced by Composer, which is to say by
// something outside this repository's control. Each has a doc comment naming a concrete bug it
// exists to prevent; these properties state that promise over the whole input space instead of
// over the one example that prompted the comment.

// jsonKeyGen generates an object key of the shape composer.json uses for a repository entry.
func jsonKeyGen() *rapid.Generator[string] {
	return rapid.StringMatching(`[a-z][a-z0-9_.-]{0,10}`)
}

func TestPropertyOrderedObjectValuesKeepsDeclarationOrder(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		keys := rapid.SliceOfN(jsonKeyGen(), 0, 6).Draw(t, "keys")

		// Values are made distinguishable by index so that a reordering is visible; sorting the
		// keys, which this function once did, would shuffle the caller's repositories into a
		// priority order the project never declared.
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

		// The disable form is one repository name mapped to a bool, and nothing else. It is
		// matched on that exact shape because a real entry may carry a bool of its own —
		// {"type":"composer","url":"...","canonical":false} is how a mirror is declared, and
		// dropping that would silently change which packages resolve.
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

		// Idempotent: the resolved URL is absolute, so normalising again must not join it onto
		// the project directory a second time. The scratch project is rebuilt per check, and a
		// doubled prefix would point at a directory that does not exist.
		again, keep := normalizeRepository(normalized, projectDir)
		require.True(t, keep)
		assert.JSONEq(t, string(normalized), string(again))
	})
}

func TestPropertyNormalizeRepositoryPassesNonPathEntriesThrough(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		repoType := rapid.SampledFrom([]string{"composer", "vcs", "git", "artifact", "package"}).Draw(t, "type")
		url := rapid.StringMatching(`https://[a-z]{1,8}\.example\.com/[a-z]{0,8}`).Draw(t, "url")

		// Only "path" repositories carry a filesystem location that the scratch project has to
		// have rewritten. Everything else must arrive byte-identical, since re-marshalling it
		// would reorder keys and could drop a field this struct does not know about.
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

		// Composer's dist-to-source fallback prints "could not be downloaded" and then carries
		// on, so an unresolvable marker can sit in output that describes a genuine patch
		// rejection. Classifying that as "could not obtain the package" would leave the package
		// unpinned and ship a patch that does not apply — the worse of the two failures, so the
		// rejection wins no matter how many markers accompany it.
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

		// Composer emits the advisories of a package either as a list or as a keyed map,
		// depending on the package. Both have to flatten into the same one list, or an advisory
		// silently disappears from a security report.
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

		// Compared as a set: the map iteration inside UnmarshalJSON has no defined order.
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

// TestPropertyAuditUnmarshalOrdersAbandonedByName states that the abandoned list does not
// depend on the order composer happened to emit its keys in. It is built by ranging over a Go
// map, whose iteration order is randomised per run, so without the sort the same audit output
// would render a different merge request description and a different report on every run — and
// a consumer diffing two reports of an unchanged site would see phantom changes.
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
