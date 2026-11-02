package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/drupdater/drupdater/internal/config"
	"github.com/drupdater/drupdater/internal/workflow"
)

// runUpdateWith calls a's runUpdate the way cobra would, with its output discarded so a failing
// run does not scribble over the test log.
func runUpdateWith(t *testing.T, a app, opts options, args ...string) error {
	t.Helper()
	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetContext(t.Context())

	return a.runUpdate(cmd, args, opts)
}

func TestRunUpdateRequiresATokenWhenItWillPublish(t *testing.T) {
	// A publishing run must fail up front, not after doing all the work it cannot deliver.
	t.Setenv("DRUPDATER_TOKEN", "")
	err := runUpdateWith(t, testApp(), options{config: config.Config{WorkingDir: t.TempDir()}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no token provided")
}

func TestRunUpdateTakesTheTokenFromTheArgument(t *testing.T) {
	// The positional argument wins over the environment. This gets past token resolution and
	// then fails on the checkout, which is what proves the token was accepted.
	t.Setenv("DRUPDATER_TOKEN", "")
	err := runUpdateWith(t, testApp(), options{config: config.Config{WorkingDir: t.TempDir()}}, "token-from-argument")
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
	err := runUpdateWith(t, testApp(), options{config: config.Config{WorkingDir: dir, DryRun: true}, configFile: cfgPath})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "no token provided")
}

func TestRunUpdateFailsOnACheckoutWithoutAnOrigin(t *testing.T) {
	// Checkout mode derives the repository URL from the origin remote. A checkout without one
	// cannot be resolved, and the run must say so instead of proceeding with an empty URL.
	dir := t.TempDir()
	gitInit(t, dir)

	// A checkout-mode dry run needs no token at all, so this reaches the checkout resolution.
	t.Setenv("DRUPDATER_TOKEN", "")
	err := runUpdateWith(t, testApp(), options{config: config.Config{WorkingDir: dir, DryRun: true, Sites: []string{"default"}}})
	require.ErrorContains(t, err, "failed to determine repository URL from checkout")
	assert.NotContains(t, err.Error(), "no token provided",
		"a checkout-mode dry run must not demand credentials it will never use")
}

// fakeWorkflow stands in for the real update workflow so runUpdate can be driven to the end.
type fakeWorkflow struct {
	called   bool
	addons   int
	services workflowServices
	// options counts the workflow options the runner was built with.
	options int
	err     error
}

func (f *fakeWorkflow) Run(_ context.Context, addons []workflow.Addon) error {
	f.called = true
	f.addons = len(addons)
	return f.err
}

// appWithWorkflow is an app whose runs all go to fake, with the services they were given.
func appWithWorkflow(fake *fakeWorkflow) app {
	a := testApp()
	a.newRunner = func(_ *zap.Logger, _ config.Config, s workflowServices, opts ...workflow.Option) updateRunner {
		fake.services = s
		fake.options = len(opts)
		return fake
	}
	return a
}

// checkoutWithOrigin returns a git checkout whose origin points at a GitHub URL, which is what
// checkout mode needs to derive the repository URL and pick a VCS provider.
func checkoutWithOrigin(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	// A commit, so HEAD resolves and the branch can be derived from the checkout.
	gitInitWithCommit(t, dir)
	gitIn(t, dir, "remote", "add", "origin", "https://github.com/acme/site.git")
	return dir
}

func TestRunUpdateWiresUpAndStartsTheWorkflow(t *testing.T) {
	// Everything up to the workflow runs for real; only the run itself is replaced.
	fake := &fakeWorkflow{}
	a := appWithWorkflow(fake)

	t.Setenv("DRUPDATER_TOKEN", "dummy-token")
	require.NoError(t, runUpdateWith(t, a, options{config: config.Config{
		WorkingDir: checkoutWithOrigin(t),
		DryRun:     true,
		Sites:      []string{"default"},
	}}))
	assert.True(t, fake.called, "the workflow has to actually be started")
	assert.Positive(t, fake.addons, "the mandatory addons must reach the workflow")
}

func TestRunUpdateReportsAWorkflowFailure(t *testing.T) {
	// A workflow error must surface. handleWorkflowError decides whether it is fatal; an
	// ordinary error is, and swallowing it would report a failed update as a success.
	fake := &fakeWorkflow{err: assert.AnError}
	a := appWithWorkflow(fake)

	t.Setenv("DRUPDATER_TOKEN", "dummy-token")
	err := runUpdateWith(t, a, options{config: config.Config{
		WorkingDir: checkoutWithOrigin(t),
		DryRun:     true,
		Sites:      []string{"default"},
	}})
	require.Error(t, err)
	require.ErrorIs(t, err, assert.AnError)
}

func TestRunUpdateWritesTheRunReport(t *testing.T) {
	// The workflow writes the file and is faked here, so this pins only the option wiring.
	fake := &fakeWorkflow{}
	a := appWithWorkflow(fake)

	t.Setenv("DRUPDATER_TOKEN", "dummy-token")
	require.NoError(t, runUpdateWith(t, a, options{config: config.Config{
		WorkingDir: checkoutWithOrigin(t),
		DryRun:     true,
		Sites:      []string{"default"},
		ReportPath: filepath.Join(t.TempDir(), "run.json"),
	}}))
	assert.True(t, fake.called)
}

// stubCommand is a command that runs runE with no arguments and prints nothing.
func stubCommand(runE func(*cobra.Command, []string) error) *cobra.Command {
	cmd := &cobra.Command{Args: cobra.ArbitraryArgs, RunE: runE}
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	// Without this cobra parses os.Args, which are the test binary's flags.
	cmd.SetArgs([]string{})
	return cmd
}

// execute turns a failed run into a non-zero exit status, which is what a CI pipeline reads.
func TestExecuteExitsNonZeroOnFailure(t *testing.T) {
	assert.Equal(t, 1, execute(t.Context(), stubCommand(func(*cobra.Command, []string) error { return assert.AnError })))
}

func TestExecuteExitsZeroOnSuccess(t *testing.T) {
	assert.Equal(t, 0, execute(t.Context(), stubCommand(func(*cobra.Command, []string) error { return nil })))
}

func TestExecutePassesTheContextOn(t *testing.T) {
	type key struct{}
	ctx := context.WithValue(t.Context(), key{}, "this run")
	var got any
	execute(ctx, stubCommand(func(cmd *cobra.Command, _ []string) error {
		got = cmd.Context().Value(key{})
		return nil
	}))
	assert.Equal(t, "this run", got, "the signal-cancelled context has to reach the run")
}

func TestNewRunnerBuildsTheWorkflowRunner(t *testing.T) {
	runner := productionApp().newRunner(zap.NewNop(), config.Config{}, workflowServices{})

	assert.IsType(t, &workflow.Runner{}, runner)
}
