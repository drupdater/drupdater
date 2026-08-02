package addon

import (
	"slices"
	"testing"

	"github.com/drupdater/drupdater/pkg/rector"
	"github.com/stretchr/testify/assert"
	"pgregory.net/rapid"
)

// recordFixes turns rector's output into a report section, and the section is supposed to be
// byte-identical for two runs over unchanged code. Rector's output order is not something this
// code controls, so "the same input in a different order gives the same section" is exactly the
// promise — and a permutation is what states it.

func rectorOutputGen() *rapid.Generator[rector.ReturnOutput] {
	fileGen := rapid.StringMatching(`(web|modules)/[a-z0-9_]{1,8}/[a-z0-9_]{1,8}\.php`)
	ruleGen := rapid.StringMatching(`Rector\\[A-Za-z]{1,10}\\[A-Za-z]{1,10}Rector`)

	return rapid.Custom(func(t *rapid.T) rector.ReturnOutput {
		files := rapid.SliceOfNDistinct(fileGen, 0, 5, rapid.ID).Draw(t, "files")

		diffs := make([]rector.ReturnOutputFillDiff, 0, len(files))
		for _, file := range files {
			// Not every changed file gets a diff entry, which is the case that decides whether
			// AppliedRectors ends up nil or populated.
			if !rapid.Bool().Draw(t, "hasDiff-"+file) {
				continue
			}
			diffs = append(diffs, rector.ReturnOutputFillDiff{
				File:           file,
				Diff:           "@@ -1 +1 @@",
				AppliedRectors: rapid.SliceOfNDistinct(ruleGen, 0, 4, rapid.ID).Draw(t, "rules-"+file),
			})
		}

		return rector.ReturnOutput{ChangedFiles: files, FileDiffs: diffs}
	})
}

func TestPropertyRecordFixesIgnoresRectorsOrdering(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		output := rectorOutputGen().Draw(t, "output")
		shuffled := rector.ReturnOutput{
			Totals:       output.Totals,
			ChangedFiles: rapid.Permutation(output.ChangedFiles).Draw(t, "changedFiles"),
			FileDiffs:    rapid.Permutation(output.FileDiffs).Draw(t, "fileDiffs"),
		}

		original := &DeprecationsRemover{}
		original.recordFixes(output)

		reordered := &DeprecationsRemover{}
		reordered.recordFixes(shuffled)

		assert.Equal(t, original.fixes, reordered.fixes)
	})
}

func TestPropertyRecordFixesCoversEveryChangedFile(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		output := rectorOutputGen().Draw(t, "output")

		remover := &DeprecationsRemover{}
		remover.recordFixes(output)

		// Every changed file is accounted for, each carries exactly the rules rector recorded
		// for it, and both levels come out ordered — that ordering is the whole reason the
		// section is stable across runs.
		assert.Len(t, remover.fixes, len(output.ChangedFiles))
		for i, fix := range remover.fixes {
			assert.ElementsMatch(t, rulesFor(output, fix.File), fix.AppliedRectors, "rules for %q", fix.File)
			if i > 0 {
				assert.LessOrEqual(t, remover.fixes[i-1].File, fix.File, "files come out sorted")
			}
			if len(fix.AppliedRectors) > 1 {
				assert.IsIncreasing(t, fix.AppliedRectors, "the rules of %q come out sorted", fix.File)
			}
		}
	})
}

func TestPropertyRecordFixesLeavesRectorsOutputAlone(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		output := rectorOutputGen().Draw(t, "output")

		// slices.Clone rather than append-onto-nil, which would turn an empty-but-not-nil
		// slice into a nil one and make the snapshot differ from the input on its own.
		before := make([][]string, len(output.FileDiffs))
		for i, diff := range output.FileDiffs {
			before[i] = slices.Clone(diff.AppliedRectors)
		}

		(&DeprecationsRemover{}).recordFixes(output)

		// The caller still owns this value — the post-code-update handler passes rector's own
		// output and goes on using it. Sorting a slice out of it in place reaches back into
		// data this function was only given to read.
		for i, diff := range output.FileDiffs {
			assert.Equal(t, before[i], diff.AppliedRectors, "AppliedRectors of %q was rewritten", diff.File)
		}
	})
}

// rulesFor returns the rules the output records for file, unsorted, or nil when the file has no
// diff entry. Deliberately not a second implementation of recordFixes: it only looks the entry
// up, and the property compares the two as sets and checks the ordering separately.
func rulesFor(output rector.ReturnOutput, file string) []string {
	for _, diff := range output.FileDiffs {
		if diff.File == file {
			return diff.AppliedRectors
		}
	}
	return nil
}
