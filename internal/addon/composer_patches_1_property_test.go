package addon

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/stretchr/testify/assert"
	"pgregory.net/rapid"
)

// Both helpers below turn untrusted input into a decision about the filesystem: cleanURLString
// builds a file name out of a drupal.org issue title, and isRemotePatch decides whether a patch
// reference names a file to remove at all. Titles are written by whoever opened the issue, so
// "for every input" is the only useful scope for their promises.

// safeFileNameChars is the character set cleanURLString promises to stay inside — no separator
// and nothing else that would break os.Create.
var safeFileNameChars = regexp.MustCompile(`^[a-z0-9._-]*$`)

func TestPropertyCleanURLStringProducesASafeFileName(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		title := rapid.String().Draw(t, "title")

		cleaned := (&ComposerPatches1{}).cleanURLString(title)

		// The result is concatenated into a path and handed to os.Create. Any title at all —
		// including one containing "../", a NUL byte or a full URL — has to come out as
		// something that cannot leave the patch directory.
		assert.Regexp(t, safeFileNameChars, cleaned, "cleaned %q", title)

		// Stated on the name the caller actually builds rather than on the fragment. A run of
		// dots survives cleaning, and that is fine: with no separator in the character set it
		// cannot form a traversal, and the fragment is always embedded between an issue ID and
		// the ".diff" suffix, so the name can never be "." or ".." either.
		const patchDir = "patches/drupal"
		name := fmt.Sprintf("%s-%s-%s.diff", "3456789", "0ff1ce", cleaned)
		assert.Equal(t, name, filepath.Base(name), "%q is not a single path element", name)
		assert.True(t, strings.HasPrefix(filepath.Clean(filepath.Join(patchDir, name)), patchDir+"/"),
			"%q escapes %q", name, patchDir)
	})
}

func TestPropertyCleanURLStringIsIdempotent(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		title := rapid.String().Draw(t, "title")

		cleaner := &ComposerPatches1{}
		once := cleaner.cleanURLString(title)
		assert.Equal(t, once, cleaner.cleanURLString(once))
	})
}

func TestPropertyIsRemotePatchRejectsEveryFilesystemPath(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		segments := rapid.SliceOfN(rapid.StringMatching(`[a-zA-Z0-9_.-]{1,10}`), 1, 4).Draw(t, "segments")
		absolute := rapid.Bool().Draw(t, "absolute")

		path := strings.Join(segments, "/")
		if absolute {
			path = "/" + path
		}

		// A local patch has a file in the worktree that has to be removed when the patch is
		// dropped. Classifying it as remote makes the removal a silent no-op, so the patch file
		// stays behind and composer-patches keeps applying it.
		assert.False(t, isRemotePatch(path), "for %q", path)
	})
}

func TestPropertyIsRemotePatchAcceptsHTTPURLs(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		scheme := rapid.SampledFrom([]string{"http", "https"}).Draw(t, "scheme")
		host := rapid.StringMatching(`[a-z][a-z0-9-]{0,10}(\.[a-z]{2,5}){1,2}`).Draw(t, "host")
		path := rapid.StringMatching(`(/[a-zA-Z0-9_.-]{1,10}){0,3}`).Draw(t, "path")

		url := scheme + "://" + host + path
		assert.True(t, isRemotePatch(url), "for %q", url)
	})
}

func TestPropertyDropPatchFileTouchesTheWorktreeOnlyForLocalPatches(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		patchPath := rapid.OneOf(
			rapid.StringMatching(`(/)?([a-zA-Z0-9_.-]{1,10}/){0,3}[a-zA-Z0-9_.-]{1,10}\.(patch|diff)`),
			rapid.StringMatching(`https?://[a-z]{1,8}\.example\.com/[a-z0-9-]{1,10}\.patch`),
		).Draw(t, "patchPath")

		worktree := NewMockWorktree(t)
		if !isRemotePatch(patchPath) {
			worktree.EXPECT().Remove(patchPath).Return(plumbing.Hash{}, nil).Once()
		}

		// A remote patch has no file in the repository, so handing its URL to worktree.Remove
		// would fail and leave the caller believing the patch could not be dropped. The two
		// have to agree for every reference, not just for the ones someone listed.
		assert.NoError(t, (&ComposerPatches1{}).dropPatchFile(worktree, patchPath))
	})
}
