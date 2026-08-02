package addon

import (
	"testing"

	"github.com/drupdater/drupdater/pkg/composer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

// GetFixedAdvisories is a set difference, and it decides what a security merge request claims to
// have fixed. Getting it wrong in the generous direction — reporting an advisory as fixed while
// it is still open — is the failure that matters, so the laws of set difference are asserted
// here rather than sampled.

// advisoryGen generates an advisory in one of the three states the key function distinguishes:
// with a CVE, with only an advisory ID, and with neither. Free-text fields draw separators on
// purpose, because that is where a hand-rolled composite key breaks.
func advisoryGen() *rapid.Generator[composer.Advisory] {
	text := rapid.StringMatching(`[a-zA-Z0-9 |:"\\-]{0,12}`)
	return rapid.Custom(func(t *rapid.T) composer.Advisory {
		return composer.Advisory{
			CVE:         rapid.StringMatching(`(CVE-20[0-9]{2}-[0-9]{4})?`).Draw(t, "cve"),
			AdvisoryID:  rapid.StringMatching(`(SA-CONTRIB-20[0-9]{2}-[0-9]{3})?`).Draw(t, "advisoryId"),
			PackageName: text.Draw(t, "packageName"),
			Title:       text.Draw(t, "title"),
			Severity:    rapid.SampledFrom([]string{"low", "medium", "high", "critical"}).Draw(t, "severity"),
		}
	})
}

func advisoriesGen() *rapid.Generator[[]composer.Advisory] {
	return rapid.SliceOfNDistinct(advisoryGen(), 0, 6, advisoryKey)
}

func TestPropertyGetFixedAdvisoriesReportsNothingWhenNothingChanged(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		advisories := advisoriesGen().Draw(t, "advisories")

		audit := &ComposerAudit{
			beforeAudit: composer.Audit{Advisories: advisories},
			afterAudit:  composer.Audit{Advisories: advisories},
		}

		assert.Empty(t, audit.GetFixedAdvisories())
	})
}

func TestPropertyGetFixedAdvisoriesReportsEverythingThatDisappeared(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		advisories := advisoriesGen().Draw(t, "advisories")

		audit := &ComposerAudit{
			beforeAudit: composer.Audit{Advisories: advisories},
			afterAudit:  composer.Audit{},
		}

		assert.Equal(t, advisories, audit.GetFixedAdvisories())
	})
}

func TestPropertyGetFixedAdvisoriesIsASubsequenceOfBefore(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		before := advisoriesGen().Draw(t, "before")
		kept := rapid.SliceOfN(rapid.Bool(), len(before), len(before)).Draw(t, "kept")

		after := make([]composer.Advisory, 0, len(before))
		want := make([]composer.Advisory, 0, len(before))
		for i, advisory := range before {
			if kept[i] {
				after = append(after, advisory)
				continue
			}
			want = append(want, advisory)
		}

		// Order preserved and nothing invented: the fixed list is exactly the advisories that
		// were there before and are not there now, in the order composer reported them.
		audit := &ComposerAudit{
			beforeAudit: composer.Audit{Advisories: before},
			afterAudit:  composer.Audit{Advisories: after},
		}
		got := audit.GetFixedAdvisories()
		assert.Equal(t, want, got)
		assert.Equal(t, got, audit.GetFixedAdvisories(), "reading the result must not consume it")
	})
}

func TestPropertyAdvisoryKeyTellsDistinctAdvisoriesApart(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		a := advisoryGen().Draw(t, "a")
		b := advisoryGen().Draw(t, "b")

		// Two advisories may share a key only when they are the same advisory. Titles are free
		// text and routinely contain the punctuation a composite key is built from, so this is
		// where an ambiguous separator shows up.
		sameIdentity := (a.CVE != "" && a.CVE == b.CVE) ||
			(a.CVE == "" && b.CVE == "" && a.AdvisoryID != "" && a.AdvisoryID == b.AdvisoryID) ||
			(a.CVE == "" && b.CVE == "" && a.AdvisoryID == "" && b.AdvisoryID == "" &&
				a.PackageName == b.PackageName && a.Title == b.Title)

		assert.Equal(t, sameIdentity, advisoryKey(a) == advisoryKey(b),
			"keys %q and %q for %+v / %+v", advisoryKey(a), advisoryKey(b), a, b)
	})
}

func TestPropertyAdvisoryKeySurvivesSeparatorsInFreeText(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		left := rapid.StringMatching(`[a-z]{1,6}`).Draw(t, "left")
		middle := rapid.StringMatching(`[a-z]{1,6}`).Draw(t, "middle")
		right := rapid.StringMatching(`[a-z]{1,6}`).Draw(t, "right")
		separator := rapid.SampledFrom([]string{"|", ":", `"`, `\`, "/"}).Draw(t, "separator")

		// Two genuinely different advisories, split at different points around the same
		// character. Any key that joins the fields on a fixed separator maps both onto one
		// string, and the second advisory is then reported as fixed while it is still open.
		// Drawing the two halves independently, as the property above does, would essentially
		// never line them up like this — the ambiguity has to be constructed to be found.
		a := composer.Advisory{PackageName: left + separator + middle, Title: right}
		b := composer.Advisory{PackageName: left, Title: middle + separator + right}

		require.NotEqual(t, a, b)
		assert.NotEqual(t, advisoryKey(a), advisoryKey(b))
	})
}
