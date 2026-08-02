package report

import (
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/drupdater/drupdater/internal/logging"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

// This file is the report's published schema, so the promises worth stating over the whole input
// space are the ones a consumer relies on: what is written parses back, no credential reaches
// the file whichever field carried it, and a reader polling the path never sees a partial write.

// secretGen generates a credential of the kind a repository URL or an error message can carry.
func secretGen() *rapid.Generator[string] {
	return rapid.StringMatching(`[a-zA-Z0-9_-]{8,24}`)
}

func TestPropertySanitizeURLDropsEveryCredential(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		user := secretGen().Draw(t, "user")
		password := secretGen().Draw(t, "password")
		host := rapid.StringMatching(`[a-z]{1,8}\.example\.com`).Draw(t, "host")
		path := rapid.StringMatching(`(/[a-z0-9_-]{1,10}){1,3}\.git`).Draw(t, "path")

		raw := fmt.Sprintf("https://%s:%s@%s%s", user, password, host, path)
		sanitized := SanitizeURL(raw)

		assert.NotContains(t, sanitized, user)
		assert.NotContains(t, sanitized, password)
		assert.Contains(t, sanitized, host, "the URL is still meant to identify the repository")
		assert.Contains(t, sanitized, path)
	})
}

func TestPropertySanitizeURLIsIdempotent(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		raw := rapid.String().Draw(t, "raw")

		once := SanitizeURL(raw)
		assert.Equal(t, once, SanitizeURL(once))
	})
}

func TestPropertySanitizeURLLeavesCredentialFreeURLsAlone(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		raw := rapid.StringMatching(`https://[a-z]{1,8}\.example\.com(:[0-9]{2,4})?(/[a-z0-9_-]{1,8}){0,3}(\?[a-z]{1,5}=[a-z0-9]{1,5})?(#[a-z]{1,5})?`).Draw(t, "raw")

		// A URL with nothing to remove has to come back byte-identical. Re-serialising it
		// through net/url would normalise escaping and case, and the report is compared across
		// runs by consumers who would read that as a change.
		assert.Equal(t, raw, SanitizeURL(raw))
	})
}

func TestPropertySanitizeURLKeepsEverythingButTheUserinfo(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		user := secretGen().Draw(t, "user")
		base := rapid.StringMatching(`https://[a-z]{1,8}\.example\.com(:[0-9]{2,4})?(/[a-z0-9_-]{1,8}){0,3}(\?[a-z]{1,5}=[a-z0-9]{1,5})?(#[a-z]{1,5})?`).Draw(t, "base")

		withUser := strings.Replace(base, "https://", "https://"+user+"@", 1)

		want, err := url.Parse(base)
		require.NoError(t, err)
		got, err := url.Parse(SanitizeURL(withUser))
		require.NoError(t, err)

		// Only the userinfo goes. Host, path, query and fragment are what make the URL useful
		// in the report at all.
		assert.Nil(t, got.User)
		assert.Equal(t, want.Host, got.Host)
		assert.Equal(t, want.Path, got.Path)
		assert.Equal(t, want.RawQuery, got.RawQuery)
		assert.Equal(t, want.Fragment, got.Fragment)
	})
}

func TestPropertyNewCheckIsOKOnlyWhenEveryResultIs(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		results := rapid.SliceOfN(rapid.Custom(func(t *rapid.T) CheckResult {
			return CheckResult{
				Name:   rapid.StringMatching(`[a-z-]{1,20}`).Draw(t, "name"),
				OK:     rapid.Bool().Draw(t, "ok"),
				Detail: rapid.StringMatching(`[a-z ]{0,20}`).Draw(t, "detail"),
			}
		}), 0, 8).Draw(t, "results")

		check := NewCheck("1.2.3", ToolVersions{}, results)

		// The document's OK mirrors the command's exit status, so a single failing check has to
		// pull it down no matter where in the list it sits.
		wantOK := true
		for _, result := range results {
			wantOK = wantOK && result.OK
		}
		assert.Equal(t, wantOK, check.OK)
		assert.Equal(t, results, check.Results, "the results are passed through untouched")
		assert.Equal(t, SchemaVersion, check.SchemaVersion)
	})
}

func TestPropertyNewCheckIgnoresResultOrder(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		results := rapid.SliceOfN(rapid.Custom(func(t *rapid.T) CheckResult {
			return CheckResult{
				Name: rapid.StringMatching(`[a-z-]{1,20}`).Draw(t, "name"),
				OK:   rapid.Bool().Draw(t, "ok"),
			}
		}), 0, 8).Draw(t, "results")
		shuffled := rapid.Permutation(results).Draw(t, "shuffled")

		assert.Equal(t, NewCheck("1.2.3", ToolVersions{}, results).OK, NewCheck("1.2.3", ToolVersions{}, shuffled).OK)
	})
}

// reportGen generates a whole report document, including the optional parts that a hand-written
// example tends to leave zero.
func reportGen(secret string) *rapid.Generator[Report] {
	text := rapid.StringMatching(`[a-z ]{0,20}`)
	return rapid.Custom(func(t *rapid.T) Report {
		rep := Report{
			SchemaVersion:    SchemaVersion,
			DrupdaterVersion: rapid.StringMatching(`(dev|[0-9]\.[0-9]\.[0-9])`).Draw(t, "version"),
			StartedAt:        time.Unix(rapid.Int64Range(0, 1<<31).Draw(t, "startedAt"), 0).UTC(),
			FinishedAt:       time.Unix(rapid.Int64Range(0, 1<<31).Draw(t, "finishedAt"), 0).UTC(),
			DurationSeconds:  float64(rapid.IntRange(0, 10000).Draw(t, "duration")),
			Status:           rapid.SampledFrom([]Status{StatusSuccess, StatusFailed, StatusNoChanges}).Draw(t, "status"),
			Mode:             rapid.SampledFrom([]Mode{ModeNormal, ModeSecurity}).Draw(t, "mode"),
			DryRun:           rapid.Bool().Draw(t, "dryRun"),
			BaseBranch:       rapid.StringMatching(`[a-z0-9/_-]{1,15}`).Draw(t, "baseBranch"),
			Sites:            rapid.SliceOfN(rapid.StringMatching(`[a-z0-9_]{1,10}`), 1, 3).Draw(t, "sites"),
			Packages: rapid.SliceOfN(rapid.Custom(func(t *rapid.T) PackageChange {
				return PackageChange{
					Action:  rapid.SampledFrom([]string{"Install", "Upgrade", "Downgrade", "Remove"}).Draw(t, "action"),
					Package: rapid.StringMatching(`[a-z0-9_-]{1,10}/[a-z0-9_-]{1,10}`).Draw(t, "package"),
					From:    rapid.StringMatching(`([0-9]\.[0-9]\.[0-9])?`).Draw(t, "from"),
					To:      rapid.StringMatching(`([0-9]\.[0-9]\.[0-9])?`).Draw(t, "to"),
				}
			}), 0, 4).Draw(t, "packages"),
			Phases: rapid.SliceOfN(rapid.Custom(func(t *rapid.T) Phase {
				return Phase{
					Name:            rapid.StringMatching(`[a-z-]{1,15}`).Draw(t, "phaseName"),
					StartedAt:       time.Unix(rapid.Int64Range(0, 1<<31).Draw(t, "phaseStartedAt"), 0).UTC(),
					DurationSeconds: float64(rapid.IntRange(0, 600).Draw(t, "phaseDuration")),
					OK:              rapid.Bool().Draw(t, "phaseOK"),
					Error:           rapid.StringMatching(`([a-z ]{1,15})?`).Draw(t, "phaseError"),
				}
			}), 0, 4).Draw(t, "phases"),
		}

		// The secret is planted in whichever field this draw picks. Which one does not matter
		// to the promise — that is the point of redacting the finished document rather than
		// naming the fields that might carry a credential.
		switch rapid.SampledFrom([]string{"repository", "error", "branch", "mr", "addons"}).Draw(t, "carrier") {
		case "repository":
			rep.Repository = "https://" + secret + "@example.com/org/repo.git"
		case "error":
			rep.FailedPhase = "composer-update"
			rep.Error = "failed to authenticate with token " + secret
		case "branch":
			rep.UpdateBranch = "update-" + secret
		case "mr":
			rep.MergeRequest = &MergeRequest{URL: "https://example.com/org/repo/-/merge_requests/" + secret}
		case "addons":
			rep.Addons = map[string]any{"composer_audit": map[string]any{"note": text.Draw(t, "note") + secret}}
		}

		return rep
	})
}

func TestPropertyWriteRoundTripsTheDocument(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		rep := reportGen("").Draw(t, "report")
		path := "/out/" + strings.Join(rapid.SliceOfN(rapid.StringMatching(`[a-z]{1,6}`), 1, 3).Draw(t, "dir"), "/") + "/report.json"

		fs := afero.NewMemMapFs()
		require.NoError(t, Write(fs, path, rep, nil))

		raw, err := afero.ReadFile(fs, path)
		require.NoError(t, err)

		// The published schema has to survive the trip: what a consumer parses back is what the
		// run recorded, for every field, not only the ones an example filled in.
		var got Report
		require.NoError(t, json.Unmarshal(raw, &got))
		assert.Equal(t, rep, got)
	})
}

func TestPropertyWriteRedactsWhicheverFieldCarriedTheSecret(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		secret := secretGen().Draw(t, "secret")
		rep := reportGen(secret).Draw(t, "report")

		redactor := logging.NewRedactor()
		redactor.Register(secret)

		fs := afero.NewMemMapFs()
		require.NoError(t, Write(fs, "/out/report.json", rep, redactor.Redact))

		raw, err := afero.ReadFile(fs, "/out/report.json")
		require.NoError(t, err)
		assert.NotContains(t, string(raw), secret)
	})
}

func TestPropertyWriteLeavesNoTemporaryFileBehind(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		rep := reportGen("").Draw(t, "report")
		dir := "/out/" + strings.Join(rapid.SliceOfN(rapid.StringMatching(`[a-z]{1,6}`), 1, 3).Draw(t, "dir"), "/")

		fs := afero.NewMemMapFs()
		require.NoError(t, Write(fs, filepath.Join(dir, "report.json"), rep, nil))

		// The write is atomic so a consumer polling the path never reads half a document. That
		// only holds if the temporary file is renamed rather than left next to the real one,
		// where a glob would pick it up.
		entries, err := afero.ReadDir(fs, dir)
		require.NoError(t, err)
		for _, entry := range entries {
			assert.NotContains(t, entry.Name(), ".drupdater-report-", "temporary file left in %s", dir)
		}
	})
}
