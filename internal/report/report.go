// Package report defines the machine-readable run report drupdater writes with --report.
//
// A run's outcome is otherwise only expressed for humans: as log lines, and as the merge request
// description assembled from each addon's RenderTemplate. Neither is a contract anything can be
// built on, and the merge request description only exists for runs that get far enough to open
// one -- a --dry-run or a run that fails during composer update leaves nothing behind but a log.
//
// The report closes that gap. It is written on every exit path, including failures, and records
// which phase failed and why. That is deliberately the most valuable case: "this repository is
// failing, in this phase, for this reason" is what anything automating drupdater across more
// than one repository needs, and it is exactly what the log-only output makes expensive to
// obtain.
package report

import (
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
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
	// StatusNoChanges means the run worked correctly and found nothing to update. It is
	// reported separately from success because the distinction is the whole point for a
	// consumer watching many repositories: a repository that is already up to date needs no
	// attention, whereas one that produced an update does.
	StatusNoChanges Status = "no_changes"
)

// Mode records whether the run applied all available updates or only security ones.
type Mode string

const (
	ModeNormal   Mode = "normal"
	ModeSecurity Mode = "security"
)

// Report is the top-level document written to the --report path.
type Report struct {
	SchemaVersion    int    `json:"schema_version"`
	DrupdaterVersion string `json:"drupdater_version"`

	StartedAt       time.Time `json:"started_at"`
	FinishedAt      time.Time `json:"finished_at"`
	DurationSeconds float64   `json:"duration_seconds"`

	Status Status `json:"status"`
	// FailedPhase names the phase that returned the error, empty on success. It is the field
	// that turns a red run into an actionable one without reading the log.
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

	// MergeRequestTitle and MergeRequestDescription are the title and body the run assembled
	// from its addons, recorded whether or not a merge request was opened with them. A
	// --dry-run has no MergeRequest at all, and these two are then the only account of what the
	// run would have said about itself -- which is both what a preview is for and the only way
	// a dry run can tell a broken description template from a working one.
	MergeRequestTitle       string `json:"merge_request_title,omitempty"`
	MergeRequestDescription string `json:"merge_request_description,omitempty"`

	Sites []string `json:"sites"`

	// Packages lists every dependency change composer made. Empty for a run that failed before
	// composer update, or that found nothing to update.
	Packages []PackageChange `json:"packages"`

	// Phases records every phase the run entered, in order, with how long it took. Beyond
	// locating a failure, the timings make the cost of a run measurable without separate
	// instrumentation: the phase distribution is what says whether a run is dominated by
	// composer install, by site installs, or by Rector.
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
// It is reported because enabling auto-merge is best-effort: a failure is logged and the run
// still succeeds, since the branch is pushed and the MR exists by then. That makes the log the
// only other place the outcome appears, so without this a consumer reading the report would see
// a clean success and never learn that the MR it is waiting on will not merge itself.
type AutoMerge struct {
	// Enabled is true when the platform accepted the request.
	Enabled bool `json:"enabled"`
	// Error is why the request failed, empty when it succeeded.
	Error string `json:"error,omitempty"`
}

// PackageChange is one dependency change made by composer update.
//
// It deliberately mirrors composer.PackageChange rather than reusing it: this type is part of
// the report's published schema, so it must only change when SchemaVersion says it does. Reusing
// the composer package's struct would let an unrelated refactor there silently rename a field
// every consumer depends on.
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

// Reporter is the optional interface an addon implements to contribute a structured section to
// the report. It is deliberately separate from internal.Addon: widening that interface would
// force every addon (and its mock) to change at once, whereas addons that do not implement
// Reporter are simply absent from the report's addons map.
type Reporter interface {
	// ReportKey is the stable key this addon's section appears under. It should match the
	// addon's registry name so a report reads the same way as .drupdater.yaml.
	ReportKey() string
	// ReportData returns JSON-serialisable data describing what the addon did.
	ReportData() any
}

// Recorder accumulates a report over the course of a run. It is safe for concurrent use: sites
// are updated concurrently, so addons may record from multiple goroutines even though the
// top-level phases themselves are sequential.
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
			// Assume success and let a failing phase overwrite it, so a run that is cut short
			// without an error (a context deadline unwinding, say) is not silently reported as
			// having failed a phase it never entered.
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
// unchanged. The first failing phase sets the report's status, failed phase and error; later
// phases do not overwrite them, because the run stops at the first failure and any subsequent
// bookkeeping is not what a reader wants to be told went wrong.
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
// The workflow signals this by aborting the phase that would have produced the update, so the
// phase is already on record as having ended with an error. That detail stays in Phases, which
// is the blow-by-blow account, but the top-level status, failed phase and error are reset:
// summarising "there was nothing to update" as a failure would have every consumer treat a
// perfectly healthy repository as one needing attention.
func (r *Recorder) SetNoChanges() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.report.Status = StatusNoChanges
	r.report.FailedPhase = ""
	r.report.Error = ""
}

// SetPackages records the dependency changes composer made.
func (r *Recorder) SetPackages(changes []PackageChange) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.report.Packages = changes
}

// SetUpdateBranch records the branch the update commits were made on. It exists even for runs
// that never push it.
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

// SetMergeRequestContent records the title and description assembled for the merge request. It
// is deliberately independent of SetMergeRequest: the content exists as soon as it is rendered,
// which happens before -- and, under --dry-run, without -- a merge request being created.
func (r *Recorder) SetMergeRequestContent(title, description string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.report.MergeRequestTitle = title
	r.report.MergeRequestDescription = description
}

// SetAutoMerge records the outcome of the auto-merge request. err is nil when the platform
// accepted it. It is a no-op when no merge request was recorded, since auto-merge is only ever
// requested for one that exists.
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

// AddAddons collects a structured section from every addon that implements Reporter. Addons that
// do not are skipped. A nil ReportData is skipped too, so an addon that ran but had nothing to
// say does not add an empty key.
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

// Check is the document "drupdater check --report" writes. It is a distinct shape from Report
// because a preflight is not a run: there are no phases, no packages and no branch, only a list
// of prerequisites and whether each one holds.
type Check struct {
	SchemaVersion    int       `json:"schema_version"`
	DrupdaterVersion string    `json:"drupdater_version"`
	CheckedAt        time.Time `json:"checked_at"`
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
func NewCheck(version string, results []CheckResult) Check {
	ok := true
	for _, r := range results {
		if !r.OK {
			ok = false
			break
		}
	}

	return Check{
		SchemaVersion:    SchemaVersion,
		DrupdaterVersion: version,
		CheckedAt:        time.Now(),
		OK:               ok,
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
// redact is applied to the marshalled document as a whole rather than to individual fields: the
// report must never carry a credential, and filtering the finished JSON means a value that
// reaches it through a field nobody thought about -- an error string quoting an authenticated
// URL, say -- is caught anyway. Pass logging.Redactor.Redact; a nil redact writes the document
// unfiltered and is only appropriate in tests.
//
// The write is atomic: the document is written to a temporary file in the destination directory
// and renamed into place, so a reader polling for the report never observes a partial one.
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

// SanitizeURL strips any credentials embedded in a URL's userinfo, so a repository URL of the
// form https://user:token@host/repo.git cannot carry a secret into the report. Values that do
// not parse as a URL are returned unchanged: they are not URLs, so there is no userinfo to
// remove, and mangling them would lose information for no gain.
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
