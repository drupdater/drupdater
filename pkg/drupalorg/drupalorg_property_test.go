package drupalorg

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"pgregory.net/rapid"
)

// FindIssueNumber pulls an issue ID out of free-form text — a patch URL, a patch description, a
// branch name — so the wrong answer silently attributes a patch to somebody else's issue. The
// properties below pin what it extracts from arbitrary surroundings, including the cases that
// are deliberate rather than accidental.

// nonDigitGen generates filler that cannot contribute digits of its own, so a generated issue
// number stays exactly the number the property put there.
func nonDigitGen() *rapid.Generator[string] {
	return rapid.StringMatching(`[a-zA-Z_./ +:?&=-]{0,20}`)
}

// issueNumberGen generates a number long enough to be recognised as an issue ID.
func issueNumberGen() *rapid.Generator[string] {
	return rapid.StringMatching(`[1-9][0-9]{5,9}`)
}

func TestPropertyFindIssueNumberExtractsTheNumberFromAnySurroundings(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		before := nonDigitGen().Draw(t, "before")
		number := issueNumberGen().Draw(t, "number")
		after := nonDigitGen().Draw(t, "after")

		got, found := (&HTTPClient{}).FindIssueNumber(before + number + after)
		assert.True(t, found)
		assert.Equal(t, number, got, "the whole run of digits is the issue ID, not a prefix of it")
	})
}

func TestPropertyFindIssueNumberTakesTheFirstMatch(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		first := issueNumberGen().Draw(t, "first")
		second := issueNumberGen().Draw(t, "second")
		between := nonDigitGen().Filter(func(s string) bool { return s != "" }).Draw(t, "between")

		// A patch file name routinely carries both the issue and the comment number
		// ("1234567-89-fix.patch"); the issue comes first, and that is the one meant.
		got, found := (&HTTPClient{}).FindIssueNumber(first + between + second)
		assert.True(t, found)
		assert.Equal(t, first, got)
	})
}

func TestPropertyFindIssueNumberIgnoresShortDigitRuns(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		parts := rapid.SliceOfN(rapid.StringMatching(`[0-9]{1,5}`), 0, 6).Draw(t, "runs")
		separators := rapid.SliceOfN(nonDigitGen().Filter(func(s string) bool {
			return s != "" && !strings.ContainsAny(s, "0123456789")
		}), len(parts), len(parts)).Draw(t, "separators")

		// Version numbers, comment counts and dates all produce short digit runs. None of them
		// is an issue ID, and treating one as such would point a patch at a random issue.
		var text strings.Builder
		for i, part := range parts {
			text.WriteString(part)
			text.WriteString(separators[i])
		}

		got, found := (&HTTPClient{}).FindIssueNumber(text.String())
		assert.False(t, found)
		assert.Empty(t, got)
	})
}
