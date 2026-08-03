package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/drupdater/drupdater/internal/codehosting"
	"go.uber.org/zap"
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
	// A bare checkout with no Drupal project: every project-dependent check fails, which is
	// the command's unhappy path end to end.
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

// stubPlatform is a codehosting.Platform whose GetUser answer the test controls, so the token
// check can be exercised without a live GitHub or GitLab API call.
type stubPlatform struct {
	name  string
	email string
}

func (s stubPlatform) CreateMergeRequest(context.Context, string, string, string, string) (codehosting.MergeRequest, error) {
	return codehosting.MergeRequest{}, nil
}
func (s stubPlatform) DeleteBranch(context.Context, string) error                      { return nil }
func (s stubPlatform) GetUser(context.Context) (string, string)                        { return s.name, s.email }
func (s stubPlatform) EnableAutoMerge(context.Context, codehosting.MergeRequest) error { return nil }

func withVcsProvider(t *testing.T, platform codehosting.Platform, err error) {
	t.Helper()
	old := newVcsProvider
	newVcsProvider = func(string, string, *zap.Logger) (codehosting.Platform, error) {
		return platform, err
	}
	t.Cleanup(func() { newVcsProvider = old })
}

func TestCheckVCSTokenBranch(t *testing.T) {
	const url = "https://github.com/acme/site.git"
	logger := zap.NewNop()

	t.Run("a token that authenticates passes", func(t *testing.T) {
		withVcsProvider(t, stubPlatform{name: "bot", email: "bot@example.com"}, nil)

		results := checkVCS(t.Context(), logger, url, "tok", nil)
		require.Len(t, results, 2, "a token adds the authentication check")
		assert.True(t, results[1].OK)
		assert.Equal(t, "token authenticates", results[1].Name)
		assert.Empty(t, results[1].Detail)
	})

	t.Run("a token the platform does not recognise fails", func(t *testing.T) {
		// Both identity fields empty is how a token without API access comes back -- the call
		// succeeds but returns nothing, so an OK here would pass a token that cannot be used.
		withVcsProvider(t, stubPlatform{}, nil)

		results := checkVCS(t.Context(), logger, url, "tok", nil)
		require.Len(t, results, 2)
		assert.False(t, results[1].OK)
		assert.Contains(t, results[1].Detail, "did not authenticate")
	})

	t.Run("an email alone still counts as authenticated", func(t *testing.T) {
		// GitHub apps report no user name but do return an email; treating that as a failure
		// would reject a perfectly usable token.
		withVcsProvider(t, stubPlatform{email: "bot@example.com"}, nil)

		results := checkVCS(t.Context(), logger, url, "tok", nil)
		require.Len(t, results, 2)
		assert.True(t, results[1].OK)
	})

	t.Run("a provider that cannot be built fails the check", func(t *testing.T) {
		withVcsProvider(t, nil, assert.AnError)

		results := checkVCS(t.Context(), logger, url, "tok", nil)
		require.Len(t, results, 2)
		assert.False(t, results[1].OK)
		assert.Equal(t, "token authenticates", results[1].Name)
		assert.NotEmpty(t, results[1].Detail)
	})
}
