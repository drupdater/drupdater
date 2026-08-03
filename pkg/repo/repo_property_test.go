package repo

import (
	"path"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"pgregory.net/rapid"
)

// pathWithin decides whether a changed file counts as a change to a configured directory. These
// properties state the containment laws for every path, not just the documented example.

// segmentGen starts each segment with an alphanumeric so it cannot produce "." or "..": git
// status paths never contain either, and path.Join would silently collapse them.
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
		// A suffix with no separator makes a different directory — "translations-backup" —
		// whose every file a substring test would report as a change to dir.
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

		// Callers pass a directory from configuration, where "/translations/" is a matter of
		// taste, but git status paths never carry the slashes.
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
