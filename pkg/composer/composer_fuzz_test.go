package composer

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// `composer audit` output is flattened by hand across the two shapes composer emits, from a
// subprocess whose version the project pins rather than drupdater. The property test states the
// laws over generated JSON; fuzzing drops the generator's structure and works on arbitrary bytes.
func FuzzAuditUnmarshalJSON(f *testing.F) {
	for _, seed := range []string{
		`{"advisories":{"drupal/core":[{"advisoryId":"PKSA-1","cve":"CVE-1","packageName":"drupal/core"}]}}`,
		`{"advisories":{"drupal/core":{"k":{"advisoryId":"PKSA-2","packageName":"drupal/core"}}}}`,
		`{"advisories":{"z/one":[{"advisoryId":"Z"}],"a/two":[{"advisoryId":"A"}]}}`,
		`{"advisories":{"drupal/core":"unexpected"}}`,
		`{"abandoned":{"swiftmailer/swiftmailer":"symfony/mailer","patchwork/jsqueeze":null}}`,
		`{"abandoned":[]}`,
		`{"advisories":{},"abandoned":{}}`,
		`{}`,
		`[]`,
		`null`,
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data string) {
		var audit Audit
		if err := json.Unmarshal([]byte(data), &audit); err != nil {
			return
		}

		// Both lists are published — the run report, the merge request description, and the
		// package list a security run hands to `composer update`. Ranging a Go map is random,
		// so an unsorted list means the same audit output produces a different run every time.
		require.True(t, slices.IsSortedFunc(audit.Advisories, compareAdvisories),
			"advisories came out unsorted, so the report order varies between identical runs")
		require.True(t, slices.IsSortedFunc(audit.Abandoned, func(a, b AbandonedPackage) int {
			return strings.Compare(a.PackageName, b.PackageName)
		}), "abandoned packages came out unsorted")
	})
}
