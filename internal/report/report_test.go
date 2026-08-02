package report

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/drupdater/drupdater/internal"
	"github.com/gookit/event"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestRecorder() *Recorder {
	return NewRecorder("1.2.3", ModeNormal, false, "https://example.com/org/site.git", "main", []string{"default"})
}

func TestRecorderRecordsSuccessfulPhases(t *testing.T) {
	rec := newTestRecorder()

	require.NoError(t, rec.Run("composer install", func() error { return nil }))
	require.NoError(t, rec.Run("site update", func() error { return nil }))

	rep := rec.Finish()

	assert.Equal(t, StatusSuccess, rep.Status)
	assert.Empty(t, rep.FailedPhase)
	assert.Empty(t, rep.Error)
	require.Len(t, rep.Phases, 2)
	assert.Equal(t, "composer install", rep.Phases[0].Name)
	assert.True(t, rep.Phases[0].OK)
	assert.Equal(t, "site update", rep.Phases[1].Name)
	assert.Equal(t, SchemaVersion, rep.SchemaVersion)
	assert.Equal(t, "1.2.3", rep.DrupdaterVersion)
	assert.False(t, rep.FinishedAt.Before(rep.StartedAt))
	assert.GreaterOrEqual(t, rep.DurationSeconds, 0.0)
}

func TestRecorderRecordsFailingPhaseAndReturnsError(t *testing.T) {
	rec := newTestRecorder()
	sentinel := errors.New("composer blew up")

	err := rec.Run("composer update", func() error { return sentinel })

	require.ErrorIs(t, err, sentinel, "Run must return the phase's error unchanged")

	rep := rec.Finish()
	assert.Equal(t, StatusFailed, rep.Status)
	assert.Equal(t, "composer update", rep.FailedPhase)
	assert.Equal(t, "composer blew up", rep.Error)
	require.Len(t, rep.Phases, 1)
	assert.False(t, rep.Phases[0].OK)
	assert.Equal(t, "composer blew up", rep.Phases[0].Error)
}

// The run stops at the first failure, but deferred bookkeeping can still record phases after it.
// The report must keep pointing at the original cause rather than the last thing that went wrong.
func TestRecorderKeepsFirstFailure(t *testing.T) {
	rec := newTestRecorder()

	_ = rec.Run("composer update", func() error { return errors.New("first") })
	_ = rec.Run("cleanup", func() error { return errors.New("second") })

	rep := rec.Finish()
	assert.Equal(t, "composer update", rep.FailedPhase)
	assert.Equal(t, "first", rep.Error)
	assert.Len(t, rep.Phases, 2)
}

func TestRecorderNoChangesClearsTheAbortAsFailure(t *testing.T) {
	rec := newTestRecorder()

	_ = rec.Run("update shared code", func() error { return errors.New("no changes detected") })
	rec.SetNoChanges()

	rep := rec.Finish()
	assert.Equal(t, StatusNoChanges, rep.Status)
	assert.Empty(t, rep.FailedPhase, "an abort is not something a consumer should act on")
	assert.Empty(t, rep.Error)
	// The phase itself stays on record: Phases is the detailed account, Status the summary.
	require.Len(t, rep.Phases, 1)
	assert.False(t, rep.Phases[0].OK)
}

func TestRecorderRecordsRunMetadata(t *testing.T) {
	rec := NewRecorder("dev", ModeSecurity, true, "https://example.com/org/site.git", "develop", []string{"default", "second"})
	rec.SetUpdateBranch("drupdater-2026")
	rec.SetPackages([]PackageChange{{Action: "Upgrade", Package: "drupal/core", From: "10.1.0", To: "10.2.0"}})
	rec.SetMergeRequest("https://example.com/org/site/-/merge_requests/7")

	rep := rec.Finish()

	assert.Equal(t, ModeSecurity, rep.Mode)
	assert.True(t, rep.DryRun)
	assert.Equal(t, "develop", rep.BaseBranch)
	assert.Equal(t, []string{"default", "second"}, rep.Sites)
	assert.Equal(t, "drupdater-2026", rep.UpdateBranch)
	require.Len(t, rep.Packages, 1)
	assert.Equal(t, "drupal/core", rep.Packages[0].Package)
	require.NotNil(t, rep.MergeRequest)
	assert.Equal(t, "https://example.com/org/site/-/merge_requests/7", rep.MergeRequest.URL)
}

func TestRecorderMergeRequestIsNilWhenNoneWasCreated(t *testing.T) {
	rep := newTestRecorder().Finish()
	assert.Nil(t, rep.MergeRequest)
}

// The rendered title and description are recorded on their own, without a merge request: a
// --dry-run renders both and opens nothing, and they are then the run's only readable summary.
func TestRecorderMergeRequestContentIsIndependentOfTheMergeRequest(t *testing.T) {
	rec := newTestRecorder()
	rec.SetMergeRequestContent("July 2026: Drupal Maintenance Updates", "## Dependency updates\n")

	rep := rec.Finish()
	assert.Equal(t, "July 2026: Drupal Maintenance Updates", rep.MergeRequestTitle)
	assert.Equal(t, "## Dependency updates\n", rep.MergeRequestDescription)
	assert.Nil(t, rep.MergeRequest, "content is recorded even though nothing was opened")
}

// Both keys are omitted when nothing was rendered -- a run that failed before the description
// was assembled must not claim an empty one.
func TestMergeRequestContentOmittedFromJSONWhenNotRendered(t *testing.T) {
	encoded, err := json.Marshal(newTestRecorder().Finish())
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "merge_request_title")
	assert.NotContains(t, string(encoded), "merge_request_description")

	rec := newTestRecorder()
	rec.SetMergeRequestContent("a title", "a description")
	encoded, err = json.Marshal(rec.Finish())
	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"merge_request_title":"a title"`)
	assert.Contains(t, string(encoded), `"merge_request_description":"a description"`)
}

func TestRecorderAutoMergeOutcome(t *testing.T) {
	t.Run("absent until requested", func(t *testing.T) {
		rec := newTestRecorder()
		rec.SetMergeRequest("https://example.com/mr/1")

		rep := rec.Finish()
		require.NotNil(t, rep.MergeRequest)
		assert.Nil(t, rep.MergeRequest.AutoMerge, "nil distinguishes not-requested from failed")
	})

	t.Run("success records enabled with no error", func(t *testing.T) {
		rec := newTestRecorder()
		rec.SetMergeRequest("https://example.com/mr/1")
		rec.SetAutoMerge(nil)

		rep := rec.Finish()
		require.NotNil(t, rep.MergeRequest.AutoMerge)
		assert.True(t, rep.MergeRequest.AutoMerge.Enabled)
		assert.Empty(t, rep.MergeRequest.AutoMerge.Error)
	})

	t.Run("failure records the reason", func(t *testing.T) {
		rec := newTestRecorder()
		rec.SetMergeRequest("https://example.com/mr/1")
		rec.SetAutoMerge(errors.New("auto-merge is not allowed for this repository"))

		rep := rec.Finish()
		require.NotNil(t, rep.MergeRequest.AutoMerge)
		assert.False(t, rep.MergeRequest.AutoMerge.Enabled)
		assert.Equal(t, "auto-merge is not allowed for this repository", rep.MergeRequest.AutoMerge.Error)
	})

	t.Run("no merge request means nothing to attach to", func(t *testing.T) {
		// A --dry-run never opens one, so there is no MR whose auto-merge state could be set.
		rec := newTestRecorder()
		rec.SetAutoMerge(errors.New("boom"))

		assert.Nil(t, rec.Finish().MergeRequest)
	})
}

// The auto_merge key must be omitted entirely when it was never requested, so a consumer can
// test for its presence rather than having to distinguish false-because-failed from
// false-because-unset.
func TestAutoMergeOmittedFromJSONWhenNotRequested(t *testing.T) {
	rec := newTestRecorder()
	rec.SetMergeRequest("https://example.com/mr/1")

	encoded, err := json.Marshal(rec.Finish().MergeRequest)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "auto_merge")

	rec.SetAutoMerge(nil)
	encoded, err = json.Marshal(rec.Finish().MergeRequest)
	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"auto_merge":{"enabled":true}`)
}

// reportingAddon implements both internal.Addon and Reporter.
type reportingAddon struct {
	key  string
	data any
}

func (a reportingAddon) SubscribedEvents() map[string]any { return map[string]any{} }
func (a reportingAddon) RenderTemplate() (string, error)  { return "", nil }
func (a reportingAddon) ReportKey() string                { return a.key }
func (a reportingAddon) ReportData() any                  { return a.data }

// plainAddon implements internal.Addon only, and must be absent from the report.
type plainAddon struct{}

func (plainAddon) SubscribedEvents() map[string]any { return map[string]any{} }
func (plainAddon) RenderTemplate() (string, error)  { return "", nil }

var (
	_ internal.Addon   = reportingAddon{}
	_ internal.Addon   = plainAddon{}
	_ event.Subscriber = reportingAddon{}
)

func TestRecorderCollectsOnlyReportingAddons(t *testing.T) {
	rec := newTestRecorder()

	rec.AddAddons([]internal.Addon{
		reportingAddon{key: "composer_audit", data: map[string]any{"fixed": 2}},
		plainAddon{},
		reportingAddon{key: "quiet_addon", data: nil},
	})

	rep := rec.Finish()

	require.Len(t, rep.Addons, 1, "addons without Reporter, and those returning nil, are omitted")
	assert.Contains(t, rep.Addons, "composer_audit")
	assert.NotContains(t, rep.Addons, "quiet_addon")
}

func TestRecorderAddonsAbsentWhenNoneReport(t *testing.T) {
	rec := newTestRecorder()
	rec.AddAddons([]internal.Addon{plainAddon{}})

	rep := rec.Finish()
	assert.Nil(t, rep.Addons, "an empty addons map should be omitted from the document entirely")
}

// Sites are updated concurrently, so addons may record from several goroutines at once.
func TestRecorderIsSafeForConcurrentUse(t *testing.T) {
	rec := newTestRecorder()

	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = rec.Run("phase", func() error { return nil })
			rec.SetUpdateBranch("branch")
			rec.AddAddons([]internal.Addon{reportingAddon{key: "addon", data: i}})
		}()
	}
	wg.Wait()

	assert.Len(t, rec.Finish().Phases, 20)
}

func TestWriteProducesRedactedJSON(t *testing.T) {
	const token = "super-secret-token"
	fs := afero.NewMemMapFs()
	path := "/out/nested/report.json"

	rec := newTestRecorder()
	_ = rec.Run("clone", func() error { return errors.New("auth failed for https://user:" + token + "@example.com/repo.git") })

	redact := func(s string) string { return strings.ReplaceAll(s, token, "***") }
	require.NoError(t, Write(fs, path, rec.Finish(), redact))

	raw, err := afero.ReadFile(fs, path)
	require.NoError(t, err)

	assert.NotContains(t, string(raw), token, "the redactor must be applied to the whole document")
	assert.Contains(t, string(raw), "***")

	var decoded Report
	require.NoError(t, json.Unmarshal(raw, &decoded), "the redacted document must still be valid JSON")
	assert.Equal(t, StatusFailed, decoded.Status)
	assert.Equal(t, "clone", decoded.FailedPhase)
}

func TestWriteCreatesMissingDirectories(t *testing.T) {
	fs := afero.NewMemMapFs()
	path := "/does/not/exist/yet/report.json"

	require.NoError(t, Write(fs, path, newTestRecorder().Finish(), nil))

	exists, err := afero.Exists(fs, path)
	require.NoError(t, err)
	assert.True(t, exists)
}

// The report is written atomically so a consumer polling for it never reads a half-written
// document; the temporary file must not survive.
func TestWriteLeavesNoTemporaryFileBehind(t *testing.T) {
	fs := afero.NewMemMapFs()
	path := "/out/report.json"

	require.NoError(t, Write(fs, path, newTestRecorder().Finish(), nil))

	entries, err := afero.ReadDir(fs, "/out")
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "report.json", filepath.Base(entries[0].Name()))
}

func TestWriteOverwritesAnExistingReport(t *testing.T) {
	fs := afero.NewMemMapFs()
	path := "/out/report.json"
	require.NoError(t, afero.WriteFile(fs, path, []byte("stale"), 0o644))

	require.NoError(t, Write(fs, path, newTestRecorder().Finish(), nil))

	raw, err := afero.ReadFile(fs, path)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "stale")
}

func TestWriteFailsOnReadOnlyFilesystem(t *testing.T) {
	fs := afero.NewReadOnlyFs(afero.NewMemMapFs())

	err := Write(fs, "/out/report.json", newTestRecorder().Finish(), nil)
	assert.Error(t, err)
}

// renameFailingFs fails every Rename, standing in for a destination that cannot be replaced
// (a full disk, a permission change between creating the temporary file and moving it).
type renameFailingFs struct {
	afero.Fs
}

func (renameFailingFs) Rename(_, _ string) error { return errors.New("rename refused") }

func TestWriteCleansUpWhenTheFinalMoveFails(t *testing.T) {
	base := afero.NewMemMapFs()
	fs := renameFailingFs{Fs: base}

	err := Write(fs, "/out/report.json", newTestRecorder().Finish(), nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "move report into place")

	entries, readErr := afero.ReadDir(base, "/out")
	require.NoError(t, readErr)
	assert.Empty(t, entries, "a failed write must not leave its temporary file behind")
}

// Addons hand back arbitrary values, so an addon returning something unserialisable must fail
// the write cleanly rather than panic or emit a truncated document.
func TestWriteFailsOnUnserialisableAddonData(t *testing.T) {
	rec := newTestRecorder()
	rec.AddAddons([]internal.Addon{reportingAddon{key: "broken", data: make(chan int)}})

	err := Write(afero.NewMemMapFs(), "/out/report.json", rec.Finish(), nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "encode report")
}

func TestNewCheckIsOKOnlyWhenEveryResultPasses(t *testing.T) {
	passing := NewCheck("dev", []CheckResult{{Name: "a", OK: true}, {Name: "b", OK: true}})
	assert.True(t, passing.OK)
	assert.Equal(t, SchemaVersion, passing.SchemaVersion)
	assert.Len(t, passing.Results, 2)

	failing := NewCheck("dev", []CheckResult{{Name: "a", OK: true}, {Name: "b", OK: false, Detail: "missing"}})
	assert.False(t, failing.OK)
}

func TestNewCheckWithNoResultsIsOK(t *testing.T) {
	assert.True(t, NewCheck("dev", nil).OK)
}

func TestWriteCheckProducesRedactedJSON(t *testing.T) {
	const token = "check-secret"
	fs := afero.NewMemMapFs()
	path := "/out/check.json"

	check := NewCheck("dev", []CheckResult{
		{Name: "token authenticates", OK: false, Detail: "401 for https://user:" + token + "@example.com"},
	})
	redact := func(s string) string { return strings.ReplaceAll(s, token, "***") }

	require.NoError(t, WriteCheck(fs, path, check, redact))

	raw, err := afero.ReadFile(fs, path)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), token)

	var decoded Check
	require.NoError(t, json.Unmarshal(raw, &decoded))
	assert.False(t, decoded.OK)
	require.Len(t, decoded.Results, 1)
	assert.Equal(t, "token authenticates", decoded.Results[0].Name)
}

func TestSanitizeURL(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"strips userinfo", "https://user:token@example.com/org/site.git", "https://example.com/org/site.git"},
		{"strips bare user", "https://token@example.com/org/site.git", "https://example.com/org/site.git"},
		{"leaves clean URLs alone", "https://example.com/org/site.git", "https://example.com/org/site.git"},
		{"leaves SSH-style remotes alone", "git@example.com:org/site.git", "git@example.com:org/site.git"},
		{"empty stays empty", "", ""},
		{"unparseable is returned unchanged", "://not a url", "://not a url"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, SanitizeURL(tt.in))
		})
	}
}

func TestRecorderSanitizesRepositoryURL(t *testing.T) {
	rec := NewRecorder("dev", ModeNormal, false, "https://user:token@example.com/org/site.git", "main", nil)

	rep := rec.Finish()
	assert.Equal(t, "https://example.com/org/site.git", rep.Repository)
	assert.NotContains(t, rep.Repository, "token")
}
