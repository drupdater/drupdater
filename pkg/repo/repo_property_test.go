package repo

import (
	"path"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"pgregory.net/rapid"
)

// pathWithin decides whether a changed file counts as a change to a configured directory. Its
// doc comment names the bug a substring test would have; these properties state the containment
// laws that rule that class of bug out for every path rather than for the one example.

// segmentGen generates one segment of a slash-separated repository path. Segments start with an
// alphanumeric so the generator cannot produce "." or ".."; git status paths are already
// relative to the worktree root and never contain either, and path.Join would silently collapse
// them into a shape the function is not being asked about.
func segmentGen() *rapid.Generator[string] {
	return rapid.StringMatching(`[a-z0-9][a-z0-9_.-]{0,7}`)
}

// segmentsGen generates between one and maxLen segments of a slash-separated repository path.
func segmentsGen(maxLen int) *rapid.Generator[[]string] {
	return rapid.SliceOfN(segmentGen(), 1, maxLen)
}

func TestPropertyPathWithinAcceptsEveryDescendant(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		dir := path.Join(segmentsGen(3).Draw(t, "dir")...)
		below := path.Join(segmentsGen(3).Draw(t, "below")...)

		assert.True(t, pathWithin(path.Join(dir, below), dir))
	})
}

func TestPropertyPathWithinRejectsPrefixSiblings(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		dir := path.Join(segmentsGen(3).Draw(t, "dir")...)
		// A non-empty suffix that does not start with a separator turns the last segment into a
		// different directory name — "translations" against "translations-backup". A substring
		// test would report every file in it as a change to dir.
		suffix := segmentGen().Draw(t, "suffix")
		file := segmentGen().Draw(t, "file")

		assert.False(t, pathWithin(path.Join(dir+suffix, file), dir))
	})
}

func TestPropertyPathWithinIsReflexive(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		dir := path.Join(segmentsGen(3).Draw(t, "dir")...)

		// A configured directory that is itself staged — a deleted directory, say — counts as
		// within itself, or the change would be invisible to the addon that asked about it.
		assert.True(t, pathWithin(dir, dir))
	})
}

func TestPropertyPathWithinIsTransitive(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		outer := path.Join(segmentsGen(2).Draw(t, "outer")...)
		middle := path.Join(outer, path.Join(segmentsGen(2).Draw(t, "middle")...))
		inner := path.Join(middle, path.Join(segmentsGen(2).Draw(t, "inner")...))

		assert.True(t, pathWithin(inner, middle))
		assert.True(t, pathWithin(middle, outer))
		assert.True(t, pathWithin(inner, outer), "containment has to compose")
	})
}

func TestPropertyPathWithinIgnoresSurroundingSlashes(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		dir := path.Join(segmentsGen(3).Draw(t, "dir")...)
		file := path.Join(dir, path.Join(segmentsGen(2).Draw(t, "file")...))
		lead := strings.Repeat("/", rapid.IntRange(0, 2).Draw(t, "lead"))
		trail := strings.Repeat("/", rapid.IntRange(0, 2).Draw(t, "trail"))

		// Callers pass a directory straight from configuration, where writing it as
		// "/translations/" is a matter of taste. Git status paths never carry them, so the
		// two forms have to mean the same thing.
		assert.Equal(t, pathWithin(file, dir), pathWithin(file, lead+dir+trail))
	})
}

func TestPropertyPathWithinTreatsAnEmptyDirAsNoFilter(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		file := rapid.String().Draw(t, "file")

		// "no path filter" is expressed as the empty directory, so it has to match everything —
		// including paths that are themselves empty or nothing but separators.
		assert.True(t, pathWithin(file, ""))
	})
}
