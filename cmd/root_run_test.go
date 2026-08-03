package cmd

import (
	"bytes"
	"context"
	"github.com/drupdater/drupdater/pkg/composer"
	"github.com/drupdater/drupdater/pkg/drupalorg"
	"github.com/drupdater/drupdater/pkg/drush"
	"github.com/drupdater/drupdater/pkg/repo"
	"github.com/spf13/cobra"
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
	// A publishing run must fail up front, not after doing all the work it cannot deliver.
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
	// Everything up to the workflow runs for real; only the run itself is replaced.
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
	// The workflow writes the file and is faked here, so this pins only the option wiring.
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

func TestExecuteExitsNonZeroOnFailure(t *testing.T) {
	// Execute is the process entry point: its whole job is turning a failed run into a non-zero
	// exit status, which is what a CI pipeline reads. Nothing else asserts that.
	var codes []int
	oldExit := osExit
	osExit = func(code int) { codes = append(codes, code) }
	t.Cleanup(func() { osExit = oldExit })

	oldArgs := rootCmd.Args
	oldRunE := rootCmd.RunE
	rootCmd.Args = cobra.ArbitraryArgs
	rootCmd.RunE = func(*cobra.Command, []string) error { return assert.AnError }
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetErr(&bytes.Buffer{})
	rootCmd.SetArgs([]string{})
	t.Cleanup(func() {
		rootCmd.Args, rootCmd.RunE = oldArgs, oldRunE
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
		rootCmd.SetArgs(nil)
	})

	Execute()
	assert.Equal(t, []int{1}, codes)
}

func TestExecuteDoesNotExitOnSuccess(t *testing.T) {
	var codes []int
	oldExit := osExit
	osExit = func(code int) { codes = append(codes, code) }
	t.Cleanup(func() { osExit = oldExit })

	oldArgs := rootCmd.Args
	oldRunE := rootCmd.RunE
	rootCmd.Args = cobra.ArbitraryArgs
	rootCmd.RunE = func(*cobra.Command, []string) error { return nil }
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetErr(&bytes.Buffer{})
	rootCmd.SetArgs([]string{})
	t.Cleanup(func() {
		rootCmd.Args, rootCmd.RunE = oldArgs, oldRunE
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
		rootCmd.SetArgs(nil)
	})

	Execute()
	assert.Empty(t, codes, "a successful run must not exit non-zero")
}

func TestCreateAddonsPassesEveryDependency(t *testing.T) {
	// Nothing downstream reports a missing dependency: it surfaces as a nil-pointer panic
	// mid-run.
	var got addonDeps
	oldRegistry, oldMandatory := addonRegistry, mandatoryAddons
	// Real factories, and two of them, so the same struct is shown to reach every addon.
	var second addonDeps
	realFactory := oldRegistry["composer_diff"]
	require.NotNil(t, realFactory)
	addonRegistry = map[string]func(addonDeps) internal.Addon{
		"probe":       func(d addonDeps) internal.Addon { got = d; return realFactory(d) },
		"probe_other": func(d addonDeps) internal.Addon { second = d; return realFactory(d) },
	}
	mandatoryAddons = []string{"probe", "probe_other"}
	t.Cleanup(func() { addonRegistry, mandatoryAddons = oldRegistry, oldMandatory })

	logger := zap.NewNop()
	cache, err := NewCache()
	require.NoError(t, err)
	drushSvc := drush.NewCLI(logger, cache)
	composerSvc := composer.NewCLI(logger)
	t.Cleanup(composerSvc.Cleanup)
	drupalOrgSvc := drupalorg.NewHTTPClient(logger)
	gitSvc := repo.NewGitRepositoryService(logger)

	_, err = createAddons(logger, internal.Config{}, drushSvc, composerSvc, drupalOrgSvc, gitSvc)
	require.NoError(t, err)

	assert.Same(t, logger, got.logger)
	assert.Equal(t, drushSvc, got.drush)
	assert.Equal(t, composerSvc, got.composer)
	assert.Equal(t, drupalOrgSvc, got.drupalOrg)
	assert.Equal(t, gitSvc, got.git)
	assert.Equal(t, got, second, "every addon receives the same dependency set")
}
