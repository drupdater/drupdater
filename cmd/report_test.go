package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/drupdater/drupdater/internal/logging"
	"github.com/drupdater/drupdater/internal/report"
	"github.com/drupdater/drupdater/internal/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestReportSinkWritesRedactedReport(t *testing.T) {
	const token = "s3cret-token"

	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "report.json")

	redactor := logging.NewRedactor()
	redactor.Register(token)

	core, logs := observer.New(zapcore.InfoLevel)
	sink := reportSink(zap.New(core), redactor, path)

	rec := report.NewRecorder("dev", report.ModeSecurity, false, "https://example.com/repo.git", "main", []string{"default"})
	_ = rec.Run("clone", func() error {
		return errNotAuthorized(token)
	})
	sink(rec.Finish())

	raw, err := os.ReadFile(path)
	require.NoError(t, err)

	assert.NotContains(t, string(raw), token, "the report must go through the log redactor")

	var decoded report.Report
	require.NoError(t, json.Unmarshal(raw, &decoded))
	assert.Equal(t, report.StatusFailed, decoded.Status)
	assert.Equal(t, "clone", decoded.FailedPhase)

	assert.Equal(t, 1, logs.FilterMessage("run report written").Len())
}

// A report that cannot be written must not turn a completed run into a failed command, nor mask
// the run's own error: the sink logs and moves on.
func TestReportSinkWarnsInsteadOfFailingWhenTheWriteFails(t *testing.T) {
	dir := t.TempDir()
	// A path whose parent is a regular file cannot be created.
	blocker := filepath.Join(dir, "blocker")
	require.NoError(t, os.WriteFile(blocker, []byte("not a directory"), 0o600))

	core, logs := observer.New(zapcore.WarnLevel)
	sink := reportSink(zap.New(core), logging.NewRedactor(), filepath.Join(blocker, "report.json"))

	assert.NotPanics(t, func() {
		sink(report.NewRecorder("dev", report.ModeNormal, true, "", "main", nil).Finish())
	})

	require.Equal(t, 1, logs.FilterMessage("failed to write run report").Len())
}

func TestWriteCheckReportWritesWhenPathGiven(t *testing.T) {
	path := filepath.Join(t.TempDir(), "check.json")
	core, logs := observer.New(zapcore.InfoLevel)

	writeCheckReport(zap.New(core), logging.NewRedactor(), path,
		report.ToolVersions{ComposerVersion: "2.10.2", PHPVersion: "8.3.14"},
		[]services.CheckResult{
			{Name: "git history complete", OK: true},
			{Name: "platform requirements", OK: false, Detail: "php 8.4 required"},
		})

	raw, err := os.ReadFile(path)
	require.NoError(t, err)

	var decoded report.Check
	require.NoError(t, json.Unmarshal(raw, &decoded))
	assert.False(t, decoded.OK, "one failing check makes the whole preflight not OK")
	assert.Len(t, decoded.Results, 2)
	assert.Equal(t, "2.10.2", decoded.ComposerVersion, "the check document says which composer it checked against")
	assert.Equal(t, "8.3.14", decoded.PHPVersion)
	assert.Equal(t, 1, logs.FilterMessage("check report written").Len())
}

// Without --report the check command must not create any file.
func TestWriteCheckReportDoesNothingWithoutAPath(t *testing.T) {
	dir := t.TempDir()

	writeCheckReport(zap.NewNop(), logging.NewRedactor(), "", report.ToolVersions{}, []services.CheckResult{{Name: "a", OK: true}})

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestWriteCheckReportWarnsWhenTheWriteFails(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	require.NoError(t, os.WriteFile(blocker, []byte("not a directory"), 0o600))

	core, logs := observer.New(zapcore.WarnLevel)
	writeCheckReport(zap.New(core), logging.NewRedactor(), filepath.Join(blocker, "check.json"), report.ToolVersions{}, nil)

	assert.Equal(t, 1, logs.FilterMessage("failed to write check report").Len())
}

func TestToReportCheckResults(t *testing.T) {
	out := toReportCheckResults([]services.CheckResult{
		{Name: "git history complete", OK: true},
		{Name: "site \"default\": settings.php", OK: false, Detail: "not found at web/sites/default"},
	})

	require.Len(t, out, 2)
	assert.Equal(t, "git history complete", out[0].Name)
	assert.True(t, out[0].OK)
	assert.Empty(t, out[0].Detail)
	assert.False(t, out[1].OK)
	assert.Equal(t, "not found at web/sites/default", out[1].Detail)
}

func TestToReportCheckResultsWithNoResults(t *testing.T) {
	assert.Empty(t, toReportCheckResults(nil))
}

// errNotAuthorized builds an error whose message embeds the token, mirroring how git and composer
// report an authentication failure against an authenticated URL.
type authError string

func (e authError) Error() string { return string(e) }

func errNotAuthorized(token string) error {
	return authError("authentication failed for https://user:" + token + "@example.com/repo.git")
}
