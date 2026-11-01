package composer

import (
	"encoding/json"
	"fmt"
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
