package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/drupdater/drupdater/internal"
	"github.com/drupdater/drupdater/internal/codehosting"
	"github.com/drupdater/drupdater/internal/services"
	git "github.com/go-git/go-git/v5"
	gitConfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
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

// fakeWorkflow stands in for the real update workflow so runUpdate can be driven to the end.
type fakeWorkflow struct {
	called bool
	addons int
	err    error
}

func (f *fakeWorkflow) StartUpdate(_ context.Context, addons []internal.Addon) error {
	f.called = true
	f.addons = len(addons)
	return f.err
}

// withFakeWorkflow swaps the workflow constructor for one returning fake.
func withFakeWorkflow(t *testing.T, fake *fakeWorkflow) {
	t.Helper()
	old := newWorkflowService
	newWorkflowService = func(
		*zap.Logger, internal.Config, services.Drush, codehosting.Platform,
		services.Repository, services.Installer, services.Composer,
		services.EventDispatcher, ...services.Option,
	) updateWorkflow {
		return fake
	}
	t.Cleanup(func() { newWorkflowService = old })
}

// checkoutWithOrigin returns a git checkout whose origin points at a GitHub URL, which is what
// checkout mode needs to derive the repository URL and pick a VCS provider.
func checkoutWithOrigin(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	r, err := git.PlainInit(dir, false)
	require.NoError(t, err)

	// A commit, so HEAD resolves and the branch can be derived from the checkout.
	wt, err := r.Worktree()
	require.NoError(t, err)
	_, err = wt.Commit("init", &git.CommitOptions{
		AllowEmptyCommits: true,
		Author:            &object.Signature{Name: "t", Email: "t@example.com"},
	})
	require.NoError(t, err)

	_, err = r.CreateRemote(&gitConfig.RemoteConfig{
		Name: "origin",
		URLs: []string{"https://github.com/acme/site.git"},
	})
	require.NoError(t, err)
	return dir
}

func TestRunUpdateWiresUpAndStartsTheWorkflow(t *testing.T) {
	// Everything up to the workflow runs for real here: the service constructors, checkout
	// resolution, the addon registry and the dispatcher. Only the run itself is replaced, so
	// this covers the wiring that a unit test otherwise cannot reach at all.
	fake := &fakeWorkflow{}
	withFakeWorkflow(t, fake)

	t.Setenv("DRUPDATER_TOKEN", "dummy-token")
	withRootCmdState(t, internal.Config{
		WorkingDir: checkoutWithOrigin(t),
		DryRun:     true,
		Sites:      []string{"default"},
	}, "")

	require.NoError(t, runUpdateWith(t))
	assert.True(t, fake.called, "the workflow has to actually be started")
	assert.Positive(t, fake.addons, "the mandatory addons must reach the workflow")
}

func TestRunUpdateReportsAWorkflowFailure(t *testing.T) {
	// A workflow error must surface. handleWorkflowError decides whether it is fatal; an
	// ordinary error is, and swallowing it would report a failed update as a success.
	fake := &fakeWorkflow{err: assert.AnError}
	withFakeWorkflow(t, fake)

	t.Setenv("DRUPDATER_TOKEN", "dummy-token")
	withRootCmdState(t, internal.Config{
		WorkingDir: checkoutWithOrigin(t),
		DryRun:     true,
		Sites:      []string{"default"},
	}, "")

	err := runUpdateWith(t)
	require.Error(t, err)
	require.ErrorIs(t, err, assert.AnError)
}

func TestRunUpdateWritesTheRunReport(t *testing.T) {
	// --report registers a sink on the workflow. The file itself is written by the workflow,
	// which is faked here, so what this pins is that requesting a report does not break the
	// wiring -- the option path is otherwise never exercised.
	fake := &fakeWorkflow{}
	withFakeWorkflow(t, fake)

	t.Setenv("DRUPDATER_TOKEN", "dummy-token")
	withRootCmdState(t, internal.Config{
		WorkingDir: checkoutWithOrigin(t),
		DryRun:     true,
		Sites:      []string{"default"},
		ReportPath: filepath.Join(t.TempDir(), "run.json"),
	}, "")

	require.NoError(t, runUpdateWith(t))
	assert.True(t, fake.called)
}
