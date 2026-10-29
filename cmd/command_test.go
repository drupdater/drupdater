package cmd

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/drupdater/drupdater/internal"
	git "github.com/go-git/go-git/v5"
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
	_, err := git.PlainInit(dir, false)
	require.NoError(t, err)
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

	out, err := runCheckCmd(t, testApp(), options{config: internal.Config{WorkingDir: dir}, configFile: cfgFile}, "argsecret-0815")

	require.Error(t, err)
	assert.Contains(t, out, ".drupdater.yaml valid", "the config check must have printed its path")
	assert.NotContains(t, out, "envsecret-4711")
	assert.NotContains(t, out, "argsecret-0815")
}

func TestCheckResolvesTheCheckoutOnlyInCheckoutMode(t *testing.T) {
	const unresolved = "failed to determine repository URL from checkout"
	dir := bareCheckout(t, t.TempDir())

	out, err := runCheckCmd(t, testApp(), options{config: internal.Config{WorkingDir: dir}})
	require.Error(t, err)
	assert.Contains(t, out, unresolved, "a checkout without origin cannot name its repository")

	out, err = runCheckCmd(t, testApp(), options{config: internal.Config{WorkingDir: dir, Clone: true, RepositoryURL: "https://github.com/acme/site.git"}})
	require.Error(t, err)
	assert.NotContains(t, out, unresolved, "--clone names the repository itself")
	assert.Contains(t, out, "✓ repository host recognized")
}

func TestCheckRunsTheFullTierOnlyWhenAskedTo(t *testing.T) {
	a, _ := appWithFullCheckDoubles(t.TempDir(), nil, &fakeComposer{}, &fakeInstaller{}, nil)
	dir := bareCheckout(t, t.TempDir())
	cfg := internal.Config{WorkingDir: dir, Clone: true, RepositoryURL: "https://github.com/acme/site.git", Sites: []string{"default"}}

	out, _ := runCheckCmd(t, a, options{config: cfg})
	assert.NotContains(t, out, "composer install")

	out, _ = runCheckCmd(t, a, options{config: cfg, checkFull: true})
	assert.Contains(t, out, "✓ composer install")
}

func TestValidateFlagsAcceptsCloneWithARepositoryURL(t *testing.T) {
	require.NoError(t, validateFlags(internal.Config{Clone: true, RepositoryURL: "https://github.com/drupdater/drupdater.git"}))
}

func TestValidateFlagsKeepsTheURLParsersReason(t *testing.T) {
	err := validateFlags(internal.Config{RepositoryURL: "not a url"})
	require.Error(t, err)
	assert.Error(t, errors.Unwrap(err), "the reason the URL was rejected must survive the wrapping")
}

func TestRunUpdateTrustsOnlyACheckoutItUpdatesInPlace(t *testing.T) {
	t.Setenv("DRUPDATER_TOKEN", "dummy-token")
	trust := func(a app) (app, *[]string) {
		var trusted []string
		a.trustCheckout = func(_ context.Context, _ *zap.Logger, dir string) { trusted = append(trusted, dir) }
		return a, &trusted
	}

	t.Run("checkout mode trusts the checkout", func(t *testing.T) {
		dir := checkoutWithOrigin(t)
		a, trusted := trust(appWithWorkflow(&fakeWorkflow{}))

		require.NoError(t, runUpdateWith(t, a, options{config: internal.Config{WorkingDir: dir, DryRun: true}}))
		assert.Equal(t, []string{dir}, *trusted)
	})

	t.Run("clone mode trusts nothing", func(t *testing.T) {
		a, trusted := trust(appWithWorkflow(&fakeWorkflow{}))

		require.NoError(t, runUpdateWith(t, a, options{config: internal.Config{
			WorkingDir: t.TempDir(), Clone: true, RepositoryURL: "https://github.com/acme/site.git", DryRun: true,
		}}))
		assert.Empty(t, *trusted, "a clone is drupdater's own, so git already trusts it")
	})
}

func TestRunUpdateHandsTheWorkflowEveryService(t *testing.T) {
	t.Setenv("DRUPDATER_TOKEN", "dummy-token")
	fake := &fakeWorkflow{}

	require.NoError(t, runUpdateWith(t, appWithWorkflow(fake), options{config: internal.Config{WorkingDir: checkoutWithOrigin(t)}}))

	assert.NotNil(t, fake.services.drush)
	assert.NotNil(t, fake.services.platform, "a publishing run needs its platform")
	assert.NotNil(t, fake.services.git)
	assert.NotNil(t, fake.services.installer)
	assert.NotNil(t, fake.services.composer)
}

func TestRunUpdateBuildsNoPlatformForATokenFreeDryRun(t *testing.T) {
	t.Setenv("DRUPDATER_TOKEN", "")
	fake := &fakeWorkflow{}

	require.NoError(t, runUpdateWith(t, appWithWorkflow(fake), options{config: internal.Config{WorkingDir: checkoutWithOrigin(t), DryRun: true}}))

	assert.Nil(t, fake.services.platform, "a checkout-mode dry run neither clones nor publishes")
}
