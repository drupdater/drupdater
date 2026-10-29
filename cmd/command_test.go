package cmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/drupdater/drupdater/internal"
	git "github.com/go-git/go-git/v5"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// executeTree runs a fresh command tree with args, returning everything it printed.
func executeTree(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	root := newRootCmd()
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
	root := newRootCmd()
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
	withFakeWorkflow(t, fake)
	t.Setenv("DRUPDATER_TOKEN", "dummy-token")

	_, err := executeTree(t, "--working-dir", checkoutWithOrigin(t), "--dry-run")

	require.NoError(t, err)
	assert.True(t, fake.called, "the flags have to reach the run")
}

func TestCheckCommandTakesAtMostOneArgument(t *testing.T) {
	dir := bareCheckout(t, t.TempDir())

	_, err := executeTree(t, "check", "--working-dir", dir, "one", "two")
	require.ErrorContains(t, err, "accepts at most 1 arg")

	_, err = executeTree(t, "check", "--working-dir", dir, "token")
	require.ErrorContains(t, err, "preflight check failed", "a single token argument is accepted")
}

func TestCheckCommandFailsWithoutPrintingUsage(t *testing.T) {
	out, err := executeTree(t, "check", "--working-dir", bareCheckout(t, t.TempDir()))

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

	out, err := runCheckCmd(t, internal.Config{WorkingDir: dir}, cfgFile, false, "argsecret-0815")

	require.Error(t, err)
	assert.Contains(t, out, ".drupdater.yaml valid", "the config check must have printed its path")
	assert.NotContains(t, out, "envsecret-4711")
	assert.NotContains(t, out, "argsecret-0815")
}

func TestCheckResolvesTheCheckoutOnlyInCheckoutMode(t *testing.T) {
	const unresolved = "failed to determine repository URL from checkout"
	dir := bareCheckout(t, t.TempDir())

	out, err := runCheckCmd(t, internal.Config{WorkingDir: dir}, "", false)
	require.Error(t, err)
	assert.Contains(t, out, unresolved, "a checkout without origin cannot name its repository")

	out, err = runCheckCmd(t, internal.Config{WorkingDir: dir, Clone: true, RepositoryURL: "https://github.com/acme/site.git"}, "", false)
	require.Error(t, err)
	assert.NotContains(t, out, unresolved, "--clone names the repository itself")
	assert.Contains(t, out, "✓ repository host recognized")
}

func TestCheckRunsTheFullTierOnlyWhenAskedTo(t *testing.T) {
	stubFullCheckDeps(t, t.TempDir(), nil, &fakeComposer{}, &fakeInstaller{}, nil)
	dir := bareCheckout(t, t.TempDir())
	cfg := internal.Config{WorkingDir: dir, Clone: true, RepositoryURL: "https://github.com/acme/site.git", Sites: []string{"default"}}

	out, _ := runCheckCmd(t, cfg, "", false)
	assert.NotContains(t, out, "composer install")

	out, _ = runCheckCmd(t, cfg, "", true)
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
