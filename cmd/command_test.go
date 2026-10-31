package cmd

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"

	"github.com/drupdater/drupdater/internal/config"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// executeTree runs a fresh command tree of a's with args, returning everything it printed.
func executeTree(t *testing.T, a app, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	root := a.newRootCmd()
	root.SetArgs(args)
	root.SetOut(&out)
	root.SetErr(&out)
	err := root.ExecuteContext(t.Context())
	return out.String(), err
}

// bareCheckout is a git repository with no origin and no Drupal project in it.
func bareCheckout(t *testing.T, dir string) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o750))
	gitInit(t, dir)
	return dir
}

func TestEveryCommandHasHelpText(t *testing.T) {
	root := testApp().newRootCmd()
	for _, cmd := range append([]*cobra.Command{root}, root.Commands()...) {
		assert.NotEmpty(t, cmd.Use, "a command without a name cannot be invoked")
		assert.NotEmpty(t, cmd.Short, "%s has no summary in --help", cmd.Name())
	}
	for _, name := range []string{"drupdater", "check"} {
		cmd, _, err := root.Find([]string{name})
		require.NoError(t, err)
		assert.NotEmpty(t, cmd.Long, "%s has no description in --help", name)
	}
}

func TestRootCommandRunsTheUpdateWithTheParsedFlags(t *testing.T) {
	fake := &fakeWorkflow{}
	t.Setenv("DRUPDATER_TOKEN", "dummy-token")

	_, err := executeTree(t, appWithWorkflow(fake), "--working-dir", checkoutWithOrigin(t), "--dry-run")

	require.NoError(t, err)
	assert.True(t, fake.called, "the flags have to reach the run")
}

func TestCheckCommandTakesAtMostOneArgument(t *testing.T) {
	dir := bareCheckout(t, t.TempDir())

	_, err := executeTree(t, testApp(), "check", "--working-dir", dir, "one", "two")
	require.ErrorContains(t, err, "accepts at most 1 arg")

	_, err = executeTree(t, testApp(), "check", "--working-dir", dir, "token")
	require.ErrorContains(t, err, "preflight check failed", "a single token argument is accepted")
}

func TestCheckCommandFailsWithoutPrintingUsage(t *testing.T) {
	out, err := executeTree(t, testApp(), "check", "--working-dir", bareCheckout(t, t.TempDir()))

	require.ErrorContains(t, err, "preflight check failed")
	assert.NotContains(t, out, "Usage:", "a failed check is not a usage error")
	assert.NotContains(t, out, "Error: preflight check failed", "Execute reports the failure; cobra must not")
}

func TestCheckRedactsTheTokenAndEnvironmentSecrets(t *testing.T) {
	// The secrets sit in the config file's path, which the failing config check prints.
	t.Setenv("DRUPALCODE_ACCESS_TOKEN", "envsecret-4711")
	dir := bareCheckout(t, filepath.Join(t.TempDir(), "envsecret-4711", "argsecret-0815"))
	cfgFile := filepath.Join(dir, ".drupdater.yaml")
	require.NoError(t, os.WriteFile(cfgFile, []byte("bogus: [\n"), 0o600))

	out, err := runCheckCmd(t, testApp(), options{config: config.Config{WorkingDir: dir}, configFile: cfgFile}, "argsecret-0815")

	require.Error(t, err)
	assert.Contains(t, out, ".drupdater.yaml valid", "the config check must have printed its path")
	assert.NotContains(t, out, "envsecret-4711")
	assert.NotContains(t, out, "argsecret-0815")
}

func TestCheckResolvesTheCheckoutOnlyInCheckoutMode(t *testing.T) {
	const unresolved = "failed to determine repository URL from checkout"
	dir := bareCheckout(t, t.TempDir())

	out, err := runCheckCmd(t, testApp(), options{config: config.Config{WorkingDir: dir}})
	require.Error(t, err)
	assert.Contains(t, out, unresolved, "a checkout without origin cannot name its repository")

	out, err = runCheckCmd(t, testApp(), options{config: config.Config{WorkingDir: dir, Clone: true, RepositoryURL: "https://github.com/acme/site.git"}})
	require.Error(t, err)
	assert.NotContains(t, out, unresolved, "--clone names the repository itself")
	assert.Contains(t, out, "✓ repository host recognized")
}

func TestCheckRunsTheFullTierOnlyWhenAskedTo(t *testing.T) {
	installer := &fakeInstaller{}
	a, _ := appWithFullCheckDoubles(t.TempDir(), nil, &fakeComposer{}, installer, nil)
	dir := bareCheckout(t, t.TempDir())
	cfg := config.Config{WorkingDir: dir, Clone: true, RepositoryURL: "https://github.com/acme/site.git", Sites: []string{"default"}}

	out, _ := runCheckCmd(t, a, options{config: cfg})
	assert.NotContains(t, out, "composer install")

	out, _ = runCheckCmd(t, a, options{config: cfg, checkFull: true})
	assert.Contains(t, out, "✓ composer install")
	require.Len(t, installer.contexts, 1)
	assert.NotNil(t, installer.contexts[0], "the command's context has to reach the site installs")
}

func TestValidateFlagsAcceptsCloneWithARepositoryURL(t *testing.T) {
	require.NoError(t, validateFlags(config.Config{Clone: true, RepositoryURL: "https://github.com/drupdater/drupdater.git"}))
}

func TestValidateFlagsKeepsTheURLParsersReason(t *testing.T) {
	err := validateFlags(config.Config{RepositoryURL: "not a url"})
	require.Error(t, err)
	assert.Error(t, errors.Unwrap(err), "the reason the URL was rejected must survive the wrapping")
}

func TestRunUpdateTrustsOnlyACheckoutItUpdatesInPlace(t *testing.T) {
	t.Setenv("DRUPDATER_TOKEN", "dummy-token")
	trust := func(a app) (app, *[]string) {
		var trusted []string
		a.trustCheckout = func(ctx context.Context, _ *zap.Logger, dir string) {
			assert.NotNil(t, ctx, "trusting runs git, which the run's context has to be able to stop")
			trusted = append(trusted, dir)
		}
		return a, &trusted
	}

	t.Run("checkout mode trusts the checkout", func(t *testing.T) {
		dir := checkoutWithOrigin(t)
		a, trusted := trust(appWithWorkflow(&fakeWorkflow{}))

		require.NoError(t, runUpdateWith(t, a, options{config: config.Config{WorkingDir: dir, DryRun: true}}))
		assert.Equal(t, []string{dir}, *trusted)
	})

	t.Run("clone mode trusts nothing", func(t *testing.T) {
		a, trusted := trust(appWithWorkflow(&fakeWorkflow{}))

		require.NoError(t, runUpdateWith(t, a, options{config: config.Config{
			WorkingDir: t.TempDir(), Clone: true, RepositoryURL: "https://github.com/acme/site.git", DryRun: true,
		}}))
		assert.Empty(t, *trusted, "a clone is drupdater's own, so git already trusts it")
	})
}

func TestRunUpdateHandsTheWorkflowEveryService(t *testing.T) {
	t.Setenv("DRUPDATER_TOKEN", "dummy-token")
	fake := &fakeWorkflow{}

	require.NoError(t, runUpdateWith(t, appWithWorkflow(fake), options{config: config.Config{WorkingDir: checkoutWithOrigin(t)}}))

	assert.NotNil(t, fake.services.drush)
	assert.NotNil(t, fake.services.platform, "a publishing run needs its platform")
	assert.NotNil(t, fake.services.git)
	assert.NotNil(t, fake.services.installer)
	assert.NotNil(t, fake.services.composer)
}

func TestRunUpdateBuildsNoPlatformForATokenFreeDryRun(t *testing.T) {
	t.Setenv("DRUPDATER_TOKEN", "")
	fake := &fakeWorkflow{}

	require.NoError(t, runUpdateWith(t, appWithWorkflow(fake), options{config: config.Config{WorkingDir: checkoutWithOrigin(t), DryRun: true}}))

	assert.Nil(t, fake.services.platform, "a checkout-mode dry run neither clones nor publishes")
}

func TestProductionAppWiresEveryDependency(t *testing.T) {
	a := productionApp()

	assert.NotNil(t, a.newRunner)
	assert.NotNil(t, a.newPlatform)
	assert.NotNil(t, a.newFullCheckDeps)
	assert.NotNil(t, a.trustCheckout)
	assert.Equal(t, builtinAddons().configurable(), a.addons.configurable())
	assert.Equal(t, builtinAddons().mandatory, a.addons.mandatory)
}

func TestEveryBuiltinAddonBuildsUnderItsOwnName(t *testing.T) {
	deps := addonDeps{logger: zap.NewNop()}
	for name, factory := range builtinAddons().factories {
		built := factory(deps)
		require.NotNil(t, built, "%s builds nothing", name)
		// A report reads the way the project is configured only when the keys are the names.
		if reporter, ok := built.(interface{ ReportKey() string }); ok {
			assert.Equal(t, name, reporter.ReportKey())
		}
	}
}

func TestRootCommandTakesAtMostOneArgument(t *testing.T) {
	_, err := executeTree(t, testApp(), "one", "two")
	require.ErrorContains(t, err, "accepts at most 1 arg")
}

func TestRootCommandFailsWithoutPrintingUsage(t *testing.T) {
	t.Setenv("DRUPDATER_TOKEN", "dummy-token")

	out, err := executeTree(t, appWithWorkflow(&fakeWorkflow{err: assert.AnError}), "--working-dir", checkoutWithOrigin(t), "--dry-run")

	require.ErrorIs(t, err, assert.AnError)
	assert.NotContains(t, out, "Usage:", "a failed update is not a usage error")
	assert.NotContains(t, out, "Error:", "Execute reports the failure; cobra must not")
}

func TestBuildingTheCommandLeavesGOMAXPROCSAlone(t *testing.T) {
	// Above 1, so a command that set GOMAXPROCS to 1 shows however many CPUs the host has.
	const want = 2
	previous := runtime.GOMAXPROCS(want)
	t.Cleanup(func() { runtime.GOMAXPROCS(previous) })

	flags := testApp().newRootCmd().PersistentFlags()

	assert.Equal(t, want, runtime.GOMAXPROCS(0), "the --concurrency default must read GOMAXPROCS, not set it")
	assert.Equal(t, strconv.Itoa(want), flags.Lookup("concurrency").DefValue)
}

func TestRunUpdateWritesAReportOnlyWhenAskedTo(t *testing.T) {
	t.Setenv("DRUPDATER_TOKEN", "dummy-token")

	without := &fakeWorkflow{}
	require.NoError(t, runUpdateWith(t, appWithWorkflow(without), options{config: config.Config{WorkingDir: checkoutWithOrigin(t), DryRun: true}}))
	assert.Zero(t, without.options, "no --report, no report sink")

	with := &fakeWorkflow{}
	require.NoError(t, runUpdateWith(t, appWithWorkflow(with), options{config: config.Config{
		WorkingDir: checkoutWithOrigin(t), DryRun: true, ReportPath: filepath.Join(t.TempDir(), "run.json"),
	}}))
	assert.Equal(t, 1, with.options)
}
