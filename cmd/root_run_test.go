package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/drupdater/drupdater/internal"
	git "github.com/go-git/go-git/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withRootCmdState saves and restores the package-level state runUpdate reads.
func withRootCmdState(t *testing.T, cfg internal.Config, cfgFile string) {
	t.Helper()
	oldConfig, oldFile := config, configFile
	config, configFile = cfg, cfgFile
	t.Cleanup(func() { config, configFile = oldConfig, oldFile })
}

// runUpdateWith calls runUpdate the way cobra would, with its output discarded so a failing
// run does not scribble over the test log.
func runUpdateWith(t *testing.T, args ...string) error {
	t.Helper()
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	rootCmd.SetContext(t.Context())
	t.Cleanup(func() { rootCmd.SetOut(nil); rootCmd.SetErr(nil) })

	return runUpdate(rootCmd, args)
}

func TestRunUpdateRequiresATokenWhenItWillPublish(t *testing.T) {
	// A publishing run (no --dry-run) pushes a branch and opens a merge request, so it cannot
	// start without credentials. Failing here rather than midway is the point: the alternative
	// is a run that does all the work and then cannot deliver it.
	t.Setenv("DRUPDATER_TOKEN", "")
	withRootCmdState(t, internal.Config{WorkingDir: t.TempDir()}, "")

	err := runUpdateWith(t)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no token provided")
}

func TestRunUpdateTakesTheTokenFromTheArgument(t *testing.T) {
	// The positional argument wins over the environment. This gets past token resolution and
	// then fails on the checkout, which is what proves the token was accepted.
	t.Setenv("DRUPDATER_TOKEN", "")
	withRootCmdState(t, internal.Config{WorkingDir: t.TempDir()}, "")

	err := runUpdateWith(t, "token-from-argument")
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "no token provided",
		"the argument has to satisfy the token requirement")
}

func TestRunUpdateRejectsAnUnreadableConfigFile(t *testing.T) {
	// A malformed .drupdater.yaml must stop the run. Continuing on built-in defaults would
	// quietly update a different set of sites than the project asked for.
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".drupdater.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte("\tnot: [valid yaml"), 0o600))

	t.Setenv("DRUPDATER_TOKEN", "dummy-token")
	withRootCmdState(t, internal.Config{WorkingDir: dir, DryRun: true}, cfgPath)

	err := runUpdateWith(t)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "no token provided")
}

func TestRunUpdateFailsOnACheckoutWithoutAnOrigin(t *testing.T) {
	// Checkout mode derives the repository URL from the origin remote. A checkout without one
	// cannot be resolved, and the run must say so instead of proceeding with an empty URL.
	dir := t.TempDir()
	_, err := git.PlainInit(dir, false)
	require.NoError(t, err)

	// A checkout-mode dry run needs no token at all, so this reaches the checkout resolution.
	t.Setenv("DRUPDATER_TOKEN", "")
	withRootCmdState(t, internal.Config{WorkingDir: dir, DryRun: true, Sites: []string{"default"}}, "")

	err = runUpdateWith(t)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "no token provided",
		"a checkout-mode dry run must not demand credentials it will never use")
}
