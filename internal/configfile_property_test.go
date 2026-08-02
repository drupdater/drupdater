package internal

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
	"pgregory.net/rapid"
)

// .drupdater.yaml is written by hand, in a repository this tool has no other contact with, and
// it is decoded strictly — so every key present has to land where the documentation says, and
// every key absent has to fall back to the documented default. Those two claims are about all
// files, not about the fixtures someone happened to write.

// fileConfigGen generates a config with every key set, so a round-trip covers each of them
// rather than the ones an example filled in.
func fileConfigGen() *rapid.Generator[fileConfig] {
	addonsGen := rapid.SliceOfNDistinct(rapid.SampledFrom(defaultNormalAddons), 0, len(defaultNormalAddons), rapid.ID)

	return rapid.Custom(func(t *rapid.T) fileConfig {
		return fileConfig{
			Sites:   rapid.SliceOfNDistinct(rapid.StringMatching(`[a-z][a-z0-9_]{0,10}`), 1, 4, rapid.ID).Draw(t, "sites"),
			Timeout: flexTimeout(rapid.SampledFrom([]string{"0", "45s", "30m", "2h", "1h30m"}).Draw(t, "timeout")),
			RunTypes: RunTypesConfig{
				Normal: RunTypeConfig{
					Addons:    addonsGen.Draw(t, "normalAddons"),
					AutoMerge: rapid.Bool().Draw(t, "normalAutoMerge"),
				},
				Security: RunTypeConfig{
					Addons:    addonsGen.Draw(t, "securityAddons"),
					AutoMerge: rapid.Bool().Draw(t, "securityAutoMerge"),
				},
			},
		}
	})
}

// writePropertyConfig writes body to a .drupdater.yaml in dir and returns its path. Separate
// from writeConfig in configfile_test.go because that one takes a *testing.T and makes its own
// temporary directory per call, and a property makes hundreds of calls under a *rapid.T.
func writePropertyConfig(t require.TestingT, dir string, body string) string {
	path := filepath.Join(dir, ".drupdater.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

func TestPropertyLoadConfigFileRoundTripsEveryKey(t *testing.T) {
	dir := t.TempDir()

	rapid.Check(t, func(t *rapid.T) {
		want := fileConfigGen().Draw(t, "config")

		body, err := yaml.Marshal(want)
		require.NoError(t, err)

		var got Config
		found, err := LoadConfigFile(writePropertyConfig(t, dir, string(body)), &got)
		require.NoError(t, err)
		assert.True(t, found)

		// Strict decoding means this also proves the struct tags match what the file emits: a
		// renamed key would fail the decode rather than silently fall back to a default.
		assert.Equal(t, want.Sites, got.Sites)
		assert.Equal(t, want.RunTypes, got.RunTypes)

		wantTimeout, err := time.ParseDuration(string(want.Timeout))
		require.NoError(t, err)
		assert.Equal(t, wantTimeout, got.Timeout)
	})
}

func TestPropertyLoadConfigFileFillsInEveryAbsentKey(t *testing.T) {
	dir := t.TempDir()
	defaults := defaultFileConfig()

	rapid.Check(t, func(t *rapid.T) {
		full := fileConfigGen().Draw(t, "config")
		withSites := rapid.Bool().Draw(t, "withSites")
		withTimeout := rapid.Bool().Draw(t, "withTimeout")
		withRunTypes := rapid.Bool().Draw(t, "withRunTypes")

		// A partial file is the normal case — most projects set `sites` and nothing else — and
		// it has to resolve to a complete config, with the keys it does not mention taking the
		// documented default rather than a zero value.
		var body strings.Builder
		if withSites {
			fmt.Fprintf(&body, "sites: [%s]\n", strings.Join(full.Sites, ", "))
		}
		if withTimeout {
			fmt.Fprintf(&body, "timeout: %q\n", string(full.Timeout))
		}
		if withRunTypes {
			fmt.Fprintf(&body, "run_types:\n  normal:\n    auto_merge: %t\n  security:\n    auto_merge: %t\n",
				full.RunTypes.Normal.AutoMerge, full.RunTypes.Security.AutoMerge)
		}

		var got Config
		_, err := LoadConfigFile(writePropertyConfig(t, dir, body.String()), &got)
		require.NoError(t, err)

		if withSites {
			assert.Equal(t, full.Sites, got.Sites)
		} else {
			assert.Equal(t, defaults.Sites, got.Sites)
		}

		wantTimeout := string(defaults.Timeout)
		if withTimeout {
			wantTimeout = string(full.Timeout)
		}
		parsed, err := time.ParseDuration(wantTimeout)
		require.NoError(t, err)
		assert.Equal(t, parsed, got.Timeout)

		// Layering is per-key all the way down, not per-block: the generated run_types names
		// only auto_merge, so the addon lists inside the same block still come from the
		// defaults. A project that turns auto-merge on does not thereby switch every addon off.
		if withRunTypes {
			assert.Equal(t, full.RunTypes.Normal.AutoMerge, got.RunTypes.Normal.AutoMerge)
			assert.Equal(t, full.RunTypes.Security.AutoMerge, got.RunTypes.Security.AutoMerge)
			assert.Equal(t, defaults.RunTypes.Normal.Addons, got.RunTypes.Normal.Addons)
			assert.Equal(t, defaults.RunTypes.Security.Addons, got.RunTypes.Security.Addons)
		} else {
			assert.Equal(t, defaults.RunTypes, got.RunTypes)
		}
	})
}

func TestPropertyLoadConfigFileTreatsAnEmptyDocumentAsAbsent(t *testing.T) {
	dir := t.TempDir()

	rapid.Check(t, func(t *rapid.T) {
		// Spaces only: a tab is not whitespace as far as YAML is concerned, it is a character
		// that cannot start a token, so a tab-only document is malformed rather than empty.
		blanks := rapid.SliceOfN(rapid.SampledFrom([]string{"", " ", "   ", "# a comment", "#"}), 0, 6).Draw(t, "lines")
		body := strings.Join(blanks, "\n")

		var fromFile Config
		_, err := LoadConfigFile(writePropertyConfig(t, dir, body), &fromFile)
		require.NoError(t, err, "body %q", body)

		var fromNothing Config
		_, err = LoadConfigFile(filepath.Join(dir, "does-not-exist.yaml"), &fromNothing)
		require.NoError(t, err)

		// A file that sets no keys and no file at all are the same statement, so they must
		// produce the same config — including the defaults, not a zero value.
		assert.Equal(t, fromNothing, fromFile, "body %q", body)
	})
}

func TestPropertyFlexTimeoutAcceptsAnyGoDuration(t *testing.T) {
	dir := t.TempDir()

	rapid.Check(t, func(t *rapid.T) {
		hours := rapid.IntRange(0, 24).Draw(t, "hours")
		minutes := rapid.IntRange(0, 59).Draw(t, "minutes")
		seconds := rapid.IntRange(0, 59).Draw(t, "seconds")
		raw := fmt.Sprintf("%dh%dm%ds", hours, minutes, seconds)

		// Quoted and bare have to mean the same thing. flexTimeout exists because `timeout: 0`,
		// the documented way to disable the timeout, decodes as an int and would not fit a
		// string field.
		var quoted, bare Config
		_, err := LoadConfigFile(writePropertyConfig(t, dir, fmt.Sprintf("sites: [default]\ntimeout: %q\n", raw)), &quoted)
		require.NoError(t, err)
		_, err = LoadConfigFile(writePropertyConfig(t, dir, fmt.Sprintf("sites: [default]\ntimeout: %s\n", raw)), &bare)
		require.NoError(t, err)

		want, err := time.ParseDuration(raw)
		require.NoError(t, err)
		assert.Equal(t, want, quoted.Timeout)
		assert.Equal(t, want, bare.Timeout)
	})
}

func TestPropertyLoadConfigFileRejectsAnEmptySiteList(t *testing.T) {
	dir := t.TempDir()

	rapid.Check(t, func(t *rapid.T) {
		empty := rapid.SampledFrom([]string{"sites: []", "sites:", "sites: ~", "sites: null"}).Draw(t, "sites")
		timeout := rapid.SampledFrom([]string{"", "\ntimeout: 30m", "\ntimeout: 0"}).Draw(t, "timeout")

		// Every per-site phase iterates this list, so an empty one would skip installing the
		// database, running update hooks and exporting configuration, and still open a merge
		// request for an update nothing validated.
		var got Config
		_, err := LoadConfigFile(writePropertyConfig(t, dir, empty+timeout+"\n"), &got)
		require.Error(t, err, "for %q", empty+timeout)
		assert.Contains(t, err.Error(), "no sites configured")
	})
}

func TestPropertyCheckLegacyLayoutFlagsOnlyTheOldShape(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		config := fileConfigGen().Draw(t, "config")
		body, err := yaml.Marshal(config)
		require.NoError(t, err)

		// No false positives: a file already written in the current layout must pass, or every
		// correctly migrated project would be told to migrate again.
		assert.NoError(t, checkLegacyLayout(body))

		legacy := rapid.SampledFrom([]string{
			"addons:\n  normal: [code_beautifier]\n",
			"auto_merge:\n  security: true\n",
			"addons:\n  normal: []\nauto_merge:\n  normal: false\n",
		}).Draw(t, "legacy")
		assert.Error(t, checkLegacyLayout([]byte(legacy)), "the pre-run_types layout has to be named")
	})
}

func TestPropertyCheckLegacyLayoutStaysOutOfTheWayOfMalformedYAML(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		body := rapid.SampledFrom([]string{
			"sites: [unclosed\n",
			"\tsites: default\n",
			"a:\n b: c\n  d: e\n",
			"{{{\n",
		}).Draw(t, "body")

		// A document that does not parse is the strict decode's business, which reports it with
		// the file name and the parser's own message. Guessing at a legacy layout here would
		// replace that with advice about a key the file may not even contain.
		assert.NoError(t, checkLegacyLayout([]byte(body)))
	})
}
