package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/drupdater/drupdater/internal"
	git "github.com/go-git/go-git/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withCheckCmdState saves and restores the package-level flag state the check command reads,
// so a test can drive it without leaking configuration into the next one.
func withCheckCmdState(t *testing.T, cfg internal.Config, cfgFile string, full bool) {
	t.Helper()
	oldConfig, oldFile, oldFull := config, configFile, checkFull
	config, configFile, checkFull = cfg, cfgFile, full
	t.Cleanup(func() { config, configFile, checkFull = oldConfig, oldFile, oldFull })
}

// runCheckCmd executes the check command's RunE the way cobra does, capturing its output.
func runCheckCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	checkCmd.SetOut(&out)
	checkCmd.SetErr(&out)
	// RunE reads cmd.Context(); cobra only populates it via Execute, which the test bypasses.
	checkCmd.SetContext(t.Context())
	t.Cleanup(func() { checkCmd.SetOut(nil); checkCmd.SetErr(nil) })

	err := checkCmd.RunE(checkCmd, args)
	return out.String(), err
}

func TestCheckCommandReportsFailuresAndExitsNonZero(t *testing.T) {
	// A bare git checkout with no Drupal project in it: the cheap tier can run end to end, and
	// every check that depends on a real project fails. That is the shape of the command's
	// unhappy path, and it is what proves RunE collects results, prints them, and turns a
	// failure into a non-zero exit rather than reporting success.
	dir := t.TempDir()
	_, err := git.PlainInit(dir, false)
	require.NoError(t, err)

	reportPath := filepath.Join(t.TempDir(), "check.json")
	withCheckCmdState(t, internal.Config{WorkingDir: dir, Sites: []string{"default"}, ReportPath: reportPath}, "", false)

	out, err := runCheckCmd(t)

	require.Error(t, err, "a failing check has to gate the pipeline")
	assert.Contains(t, err.Error(), "preflight check failed")
	assert.NotEmpty(t, out, "the results have to be printed, not just returned")

	// The report is written on the failing path too -- it is what a pipeline consumes when the
	// check gate trips.
	written, readErr := os.ReadFile(reportPath) //nolint:gosec // test-controlled path
	require.NoError(t, readErr)

	var check struct {
		SchemaVersion int  `json:"schema_version"`
		OK            bool `json:"ok"`
		Results       []struct {
			Name string `json:"name"`
			OK   bool   `json:"ok"`
		} `json:"results"`
	}
	require.NoError(t, json.Unmarshal(written, &check))
	assert.NotZero(t, check.SchemaVersion)
	assert.False(t, check.OK, "the report's verdict must mirror the command's exit status")
	require.NotEmpty(t, check.Results)
	for _, c := range check.Results {
		assert.NotEmpty(t, c.Name, "every reported check needs a name")
	}
}

func TestCheckCommandWithoutReportPathWritesNoReport(t *testing.T) {
	dir := t.TempDir()
	_, err := git.PlainInit(dir, false)
	require.NoError(t, err)

	// An empty ReportPath must not write anything, rather than defaulting to some file in the
	// working directory.
	withCheckCmdState(t, internal.Config{WorkingDir: dir, Sites: []string{"default"}}, "", false)

	_, err = runCheckCmd(t)
	require.Error(t, err)

	entries, readErr := os.ReadDir(dir)
	require.NoError(t, readErr)
	for _, e := range entries {
		assert.NotContains(t, e.Name(), ".json", "no report may be written without --report")
	}
}
