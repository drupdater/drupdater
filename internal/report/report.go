// Package report defines the machine-readable run report drupdater writes with --report.
//
// A run's outcome is otherwise only expressed for humans, and the merge request description only
// exists for runs that get far enough to open one. The report is written on every exit path,
// including failures, and records which phase failed and why -- what anything automating
// drupdater across many repositories needs and the log makes expensive to obtain.
package report

import (
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/drupdater/drupdater/internal"
	"github.com/spf13/afero"
)

// SchemaVersion is the version of the report format. It is part of the report's public contract:
// new fields may be added without bumping it (consumers must ignore unknown fields), while
// removing or renaming a field requires an increment.
const SchemaVersion = 1

// Status is the overall outcome of a run.
type Status string

const (
	// StatusSuccess means the run completed every phase it attempted. A --dry-run that stopped
	// short of pushing is still a success: it did everything it was asked to do.
	StatusSuccess Status = "success"
	// StatusFailed means a phase returned an error and the run stopped there.
	StatusFailed Status = "failed"
	// StatusNoChanges means the run worked and found nothing to update. Separate from success
	// because a consumer watching many repositories needs no attention for this one.
	StatusNoChanges Status = "no_changes"
)

// Mode records whether the run applied all available updates or only security ones.
type Mode string

const (
	ModeNormal   Mode = "normal"
	ModeSecurity Mode = "security"
)

// ToolVersions records the versions of the tools whose behaviour the outcome depends on. Without
// them, a fleet of repositories failing the same phase after an upstream composer release is
// unattributable. Embedded in both documents so the two cannot drift on field names.
type ToolVersions struct {
	ComposerVersion string `json:"composer_version,omitempty"`
	PHPVersion      string `json:"php_version,omitempty"`
}

// Report is the top-level document written to the --report path.
type Report struct {
	SchemaVersion    int    `json:"schema_version"`
	DrupdaterVersion string `json:"drupdater_version"`
	// Embedded, so these land beside drupdater_version rather than nested under a key.
	ToolVersions

	StartedAt       time.Time `json:"started_at"`
	FinishedAt      time.Time `json:"finished_at"`
	DurationSeconds float64   `json:"duration_seconds"`

	Status Status `json:"status"`
	// FailedPhase names the phase that returned the error, empty on success.
	FailedPhase string `json:"failed_phase,omitempty"`
	Error       string `json:"error,omitempty"`

	Mode   Mode `json:"mode"`
	DryRun bool `json:"dry_run"`

	Repository   string `json:"repository"`
	BaseBranch   string `json:"base_branch"`
	UpdateBranch string `json:"update_branch,omitempty"`
	// MergeRequest is nil when no merge request was created -- because the run was a --dry-run,
	// or because it failed before publishing.
	MergeRequest *MergeRequest `json:"merge_request"`

	// Recorded whether or not a merge request was opened. Under --dry-run they are the only
	// account of what the run would have said, and the only way to spot a broken template.
	MergeRequestTitle       string `json:"merge_request_title,omitempty"`
	MergeRequestDescription string `json:"merge_request_description,omitempty"`

	Sites []string `json:"sites"`

	// Packages lists every dependency change composer made. Empty for a run that failed before
	// composer update, or that found nothing to update.
	Packages []PackageChange `json:"packages"`

	// Phases records every phase the run entered, in order. The timings make a run's cost
	// measurable without separate instrumentation.
	Phases []Phase `json:"phases"`

	// Addons holds each reporting addon's structured section, keyed by its report key. Addons
	// that do not implement report.Reporter are absent rather than present and empty.
	Addons map[string]any `json:"addons,omitempty"`
}

// MergeRequest identifies the merge/pull request a successful run opened.
type MergeRequest struct {
	URL string `json:"url"`
	// AutoMerge is nil when the active run type did not ask for auto-merge, so a consumer can
	// tell "not requested" apart from "requested and failed".
	AutoMerge *AutoMerge `json:"auto_merge,omitempty"`
}

// AutoMerge is the outcome of asking the platform to merge the MR/PR once its pipeline passes.
//
// Reported because enabling auto-merge is best-effort: a failure is logged and the run still
// succeeds, so without this a consumer would see a clean success and never learn the MR it is
// waiting on will not merge itself.
type AutoMerge struct {
	// Enabled is true when the platform accepted the request.
	Enabled bool `json:"enabled"`
	// Error is why the request failed, empty when it succeeded.
	Error string `json:"error,omitempty"`
}

// PackageChange mirrors composer.PackageChange rather than reusing it: this type is published
// schema, so a refactor in the composer package must not be able to rename a field here.
type PackageChange struct {
	// Action is one of Install, Upgrade, Downgrade, Remove.
	Action  string `json:"action"`
	Package string `json:"package"`
	From    string `json:"from,omitempty"`
	To      string `json:"to,omitempty"`
}

// Phase is one step of the workflow with its duration and outcome.
type Phase struct {
	Name            string    `json:"name"`
	StartedAt       time.Time `json:"started_at"`
	DurationSeconds float64   `json:"duration_seconds"`
	OK              bool      `json:"ok"`
	Error           string    `json:"error,omitempty"`
}

// Reporter is the optional interface an addon implements to contribute a section to the report.
// Kept separate from internal.Addon so adding it does not force every addon and mock to change.
type Reporter interface {
	// ReportKey is the stable key this addon's section appears under. It should match the
	// addon's registry name so a report reads the same way as .drupdater.yaml.
	ReportKey() string
	// ReportData returns JSON-serialisable data describing what the addon did.
	ReportData() any
}

// Recorder accumulates a report over a run. Safe for concurrent use: sites update concurrently,
// so addons may record from several goroutines even though the phases are sequential.
type Recorder struct {
	mu     sync.Mutex
	report Report
}

// NewRecorder starts a report for a run beginning now.
func NewRecorder(version string, mode Mode, dryRun bool, repositoryURL string, baseBranch string, sites []string) *Recorder {
	return &Recorder{
		report: Report{
			SchemaVersion:    SchemaVersion,
			DrupdaterVersion: version,
			StartedAt:        time.Now(),
			// A failing phase overwrites this, so a run cut short without an error is not
			// reported as having failed a phase it never entered.
			Status:     StatusSuccess,
			Mode:       mode,
			DryRun:     dryRun,
			Repository: SanitizeURL(repositoryURL),
			BaseBranch: baseBranch,
			Sites:      sites,
		},
	}
}

// Run executes fn as a named phase, recording its duration and outcome, and returns fn's error
// unchanged. Only the first failure sets the top-level status: later bookkeeping is not what
// went wrong.
func (r *Recorder) Run(name string, fn func() error) error {
	start := time.Now()
	err := fn()

	r.mu.Lock()
	defer r.mu.Unlock()

	phase := Phase{
		Name:            name,
		StartedAt:       start,
		DurationSeconds: time.Since(start).Seconds(),
		OK:              err == nil,
	}
	if err != nil {
		phase.Error = err.Error()
		if r.report.Status != StatusFailed {
			r.report.Status = StatusFailed
			r.report.FailedPhase = name
			r.report.Error = err.Error()
		}
	}
	r.report.Phases = append(r.report.Phases, phase)

	return err
}

// SetNoChanges records that the run found nothing to update.
//
// The workflow signals this by aborting a phase, so that phase stays on record as failed in
// Phases. The top-level status is reset, or every consumer would treat a healthy repository as
// one needing attention.
func (r *Recorder) SetNoChanges() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.report.Status = StatusNoChanges
	r.report.FailedPhase = ""
	r.report.Error = ""
}

// SetToolVersions records the versions of the tools driving the run. A setter because reading
// them costs a subprocess the recorder must already exist to outlive.
func (r *Recorder) SetToolVersions(versions ToolVersions) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.report.ToolVersions = versions
}

// SetPackages records the dependency changes composer made.
func (r *Recorder) SetPackages(changes []PackageChange) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.report.Packages = changes
}

// SetUpdateBranch records the branch the update commits were made on, even if never pushed.
func (r *Recorder) SetUpdateBranch(branch string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.report.UpdateBranch = branch
}

// SetMergeRequest records the merge request a run opened.
func (r *Recorder) SetMergeRequest(url string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.report.MergeRequest = &MergeRequest{URL: SanitizeURL(url)}
}

// SetMergeRequestContent records the assembled title and description. Independent of
// SetMergeRequest: the content exists once rendered, which is before -- and under --dry-run
// without -- any merge request.
func (r *Recorder) SetMergeRequestContent(title, description string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.report.MergeRequestTitle = title
	r.report.MergeRequestDescription = description
}

// SetAutoMerge records the outcome of the auto-merge request; err is nil when the platform
// accepted it. A no-op when no merge request was recorded.
func (r *Recorder) SetAutoMerge(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.report.MergeRequest == nil {
		return
	}
	am := &AutoMerge{Enabled: err == nil}
	if err != nil {
		am.Error = err.Error()
	}
	r.report.MergeRequest.AutoMerge = am
}

// AddAddons collects a section from every addon implementing Reporter. A nil ReportData is
// skipped too, so an addon with nothing to say does not add an empty key.
func (r *Recorder) AddAddons(addons []internal.Addon) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, a := range addons {
		reporter, ok := a.(Reporter)
		if !ok {
			continue
		}
		data := reporter.ReportData()
		if data == nil {
			continue
		}
		if r.report.Addons == nil {
			r.report.Addons = map[string]any{}
		}
		r.report.Addons[reporter.ReportKey()] = data
	}
}

// Finish closes the report and returns it. The caller may call it more than once; each call
// refreshes the end timestamp.
func (r *Recorder) Finish() Report {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.report.FinishedAt = time.Now()
	r.report.DurationSeconds = r.report.FinishedAt.Sub(r.report.StartedAt).Seconds()

	return r.report
}

// Check is the document "drupdater check --report" writes. A distinct shape from Report: a
// preflight has no phases, packages or branch, only prerequisites and whether each holds.
type Check struct {
	SchemaVersion    int    `json:"schema_version"`
	DrupdaterVersion string `json:"drupdater_version"`
	ToolVersions
	CheckedAt time.Time `json:"checked_at"`
	// OK is false when any individual check failed, mirroring the command's exit status.
	OK      bool          `json:"ok"`
	Results []CheckResult `json:"results"`
}

// CheckResult is one named prerequisite and its outcome.
type CheckResult struct {
	Name string `json:"name"`
	OK   bool   `json:"ok"`
	// Detail explains a failure; empty on a pass.
	Detail string `json:"detail,omitempty"`
}

// NewCheck assembles the check document from the results the command produced.
func NewCheck(version string, tools ToolVersions, results []CheckResult) Check {
	return Check{
		SchemaVersion:    SchemaVersion,
		DrupdaterVersion: version,
		ToolVersions:     tools,
		CheckedAt:        time.Now(),
		OK:               !slices.ContainsFunc(results, func(r CheckResult) bool { return !r.OK }),
		Results:          results,
	}
}

// WriteCheck serialises a preflight result to path, with the same atomicity and redaction
// guarantees as Write.
func WriteCheck(fs afero.Fs, path string, check Check, redact func(string) string) error {
	return writeJSON(fs, path, check, redact)
}

// Write serialises rep to path.
//
// redact runs over the whole marshalled document, not per field, so a credential reaching it
// through a field nobody thought about -- an error string quoting an authenticated URL -- is
// caught anyway. Pass logging.Redactor.Redact; nil writes unfiltered and suits only tests.
//
// The write is atomic (temp file plus rename), so a reader polling for the report never sees a
// partial one.
func Write(fs afero.Fs, path string, rep Report, redact func(string) string) error {
	return writeJSON(fs, path, rep, redact)
}

// writeJSON is the shared, atomic, redacting write behind Write and WriteCheck.
func writeJSON(fs afero.Fs, path string, doc any, redact func(string) string) error {
	encoded, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode report: %w", err)
	}
	encoded = append(encoded, '\n')

	if redact != nil {
		encoded = []byte(redact(string(encoded)))
	}

	dir := filepath.Dir(path)
	if err := fs.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create report directory %s: %w", dir, err)
	}

	tmp, err := afero.TempFile(fs, dir, ".drupdater-report-*")
	if err != nil {
		return fmt.Errorf("failed to create temporary report file: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(encoded); err != nil {
		_ = tmp.Close()
		_ = fs.Remove(tmpName)
		return fmt.Errorf("failed to write report: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = fs.Remove(tmpName)
		return fmt.Errorf("failed to close report: %w", err)
	}
	if err := fs.Rename(tmpName, path); err != nil {
		_ = fs.Remove(tmpName)
		return fmt.Errorf("failed to move report into place at %s: %w", path, err)
	}

	return nil
}

// SanitizeURL strips a URL's userinfo, so https://user:token@host/repo.git cannot carry a
// secret into the report. Values that do not parse as a URL are returned unchanged.
func SanitizeURL(raw string) string {
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User == nil {
		return raw
	}
	parsed.User = nil

	return parsed.String()
}
