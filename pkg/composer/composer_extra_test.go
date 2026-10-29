package composer

import (
	"context"
	"os"
	"os/exec"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// stubComposerOutput makes the next composer invocations print out on stdout and exit 0. It
// restores the real exec.CommandContext when the test ends.
func stubComposerOutput(t *testing.T, out string) {
	t.Helper()
	execCommand = func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
		cs := []string{"-test.run=TestHelperProcess", "--", out}
		cs = append(cs, arg...)
		cmd := exec.CommandContext(ctx, os.Args[0], cs...)
		cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1", "GOCOVERDIR=/tmp"}
		return cmd
	}
	t.Cleanup(func() { execCommand = exec.CommandContext })
}

// stubComposerFailure makes the next composer invocations exit non-zero.
func stubComposerFailure(t *testing.T) {
	t.Helper()
	execCommand = func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
		cs := []string{"-test.run=TestHelperProcess", "--"}
		cs = append(cs, arg...)
		cmd := exec.CommandContext(ctx, os.Args[0], cs...)
		cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1", "GO_HELPER_PROCESS_ERROR=1", "GOCOVERDIR=/tmp"}
		return cmd
	}
	t.Cleanup(func() { execCommand = exec.CommandContext })
}

func TestNewCLI(t *testing.T) {
	logger := zap.NewNop()
	cli := NewCLI(logger)

	require.NotNil(t, cli)
	assert.Equal(t, logger, cli.logger)
	assert.NotNil(t, cli.fs)
}

func allowPluginsFrom(t *testing.T, manifest string) (AllowPlugins, error) {
	t.Helper()
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/project/composer.json", []byte(manifest), 0o644))
	return (&CLI{logger: zap.NewNop(), fs: fs}).GetAllowPlugins("/project")
}

func TestGetAllowPluginsReadsEveryShape(t *testing.T) {
	yes, no := true, false
	for name, tt := range map[string]struct {
		manifest string
		want     AllowPlugins
	}{
		"absent":       {`{"config": {}}`, AllowPlugins{}},
		"no config":    {`{}`, AllowPlugins{}},
		"null":         {`{"config": {"allow-plugins": null}}`, AllowPlugins{}},
		"true":         {`{"config": {"allow-plugins": true}}`, AllowPlugins{Set: true, All: &yes}},
		"false":        {`{"config": {"allow-plugins": false}}`, AllowPlugins{Set: true, All: &no}},
		"empty object": {`{"config": {"allow-plugins": {}}}`, AllowPlugins{Set: true, Rules: map[string]bool{}}},
		"empty array":  {`{"config": {"allow-plugins": []}}`, AllowPlugins{Set: true, Rules: map[string]bool{}}},
		"rules": {`{"config": {"allow-plugins": {"drupal/*": true, "phpro/grumphp-shim": false}}}`,
			AllowPlugins{Set: true, Rules: map[string]bool{"drupal/*": true, "phpro/grumphp-shim": false}}},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := allowPluginsFrom(t, tt.manifest)

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGetAllowPluginsErrors(t *testing.T) {
	t.Run("no composer.json", func(t *testing.T) {
		_, err := (&CLI{logger: zap.NewNop(), fs: afero.NewMemMapFs()}).GetAllowPlugins("/project")
		require.ErrorContains(t, err, "failed to read composer.json")
	})
	t.Run("composer.json is not JSON", func(t *testing.T) {
		_, err := allowPluginsFrom(t, `{"config": `)
		require.ErrorContains(t, err, "failed to parse composer.json")
	})
	t.Run("a rule that is not a boolean", func(t *testing.T) {
		_, err := allowPluginsFrom(t, `{"config": {"allow-plugins": {"a/b": "yes"}}}`)
		require.ErrorContains(t, err, `failed to parse composer allow-plugins config {"a/b": "yes"}`)
	})
}

// The patterns behave as in Composer's PluginManager: anchored, case-insensitive, * for anything.
func TestAllowPluginsDecides(t *testing.T) {
	yes := true
	rules := AllowPlugins{Set: true, Rules: map[string]bool{"drupal/*": true, "cweagans/composer-patches": false, "a.b/c": true}}

	assert.True(t, rules.Decides("drupal/core-composer-scaffold"), "matched by a wildcard")
	assert.True(t, rules.Decides("Drupal/Core-Composer-Scaffold"), "case-insensitive")
	assert.True(t, rules.Decides("cweagans/composer-patches"), "a denial decides too")
	assert.False(t, rules.Decides("cweagans/composer-patches-extra"), "anchored at the end")
	assert.False(t, rules.Decides("acme/drupal/x"), "anchored at the start")
	assert.False(t, rules.Decides("aXb/c"), "a dot is literal")
	assert.False(t, rules.Decides("php-http/discovery"))
	assert.False(t, AllowPlugins{}.Decides("drupal/core"), "no setting decides nothing")
	assert.True(t, AllowPlugins{Set: true, All: &yes}.Decides("anything/at-all"))
}

func TestUnsetConfig(t *testing.T) {
	var args []string
	execCommand = func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
		args = arg
		cs := append([]string{"-test.run=TestHelperProcess", "--", ""}, arg...)
		cmd := exec.CommandContext(ctx, os.Args[0], cs...)
		cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1", "GOCOVERDIR=/tmp"}
		return cmd
	}
	t.Cleanup(func() { execCommand = exec.CommandContext })

	require.NoError(t, (&CLI{logger: zap.NewNop()}).UnsetConfig(t.Context(), "/tmp", "allow-plugins.*"))
	assert.Equal(t, []string{"config", "--unset", "allow-plugins.*"}, args)

	stubComposerFailure(t)
	require.Error(t, (&CLI{logger: zap.NewNop()}).UnsetConfig(t.Context(), "/tmp", "allow-plugins.*"))
}

func TestCleanup(t *testing.T) {
	t.Run("removes the scratch project", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		service := &CLI{logger: zap.NewNop(), fs: fs}

		service.initOnce.Do(service.initTempDir)
		require.NoError(t, service.initErr)
		dir := service.tempDir
		require.NotEmpty(t, dir)
		exists, err := afero.DirExists(fs, dir)
		require.NoError(t, err)
		require.True(t, exists)

		service.Cleanup()

		exists, err = afero.DirExists(fs, dir)
		require.NoError(t, err)
		assert.False(t, exists)
		assert.Empty(t, service.tempDir)
	})

	t.Run("is a no-op when no scratch project was created", func(t *testing.T) {
		service := &CLI{logger: zap.NewNop(), fs: afero.NewMemMapFs()}
		service.Cleanup()
		assert.Empty(t, service.tempDir)
	})

	t.Run("is safe to call twice", func(t *testing.T) {
		service := &CLI{logger: zap.NewNop(), fs: afero.NewMemMapFs()}
		service.initOnce.Do(service.initTempDir)
		require.NoError(t, service.initErr)

		service.Cleanup()
		service.Cleanup()
		assert.Empty(t, service.tempDir)
	})
}

func TestResetScratchProject(t *testing.T) {
	fs := afero.NewMemMapFs()
	service := &CLI{logger: zap.NewNop(), fs: fs}
	service.initOnce.Do(service.initTempDir)
	require.NoError(t, service.initErr)

	// What a prior patch check leaves behind: a pinned require, plus its lock and vendor tree.
	require.NoError(t, afero.WriteFile(fs, service.tempDir+"/composer.json", []byte(`{"require":{"some/leftover-package":"1.2.3"}}`), 0644))
	require.NoError(t, afero.WriteFile(fs, service.tempDir+"/composer.lock", []byte("{}"), 0644))
	require.NoError(t, afero.WriteFile(fs, service.tempDir+"/vendor/autoload.php", []byte("<?php"), 0644))

	require.NoError(t, service.resetScratchProject("/project"))

	content, err := afero.ReadFile(fs, service.tempDir+"/composer.json")
	require.NoError(t, err)
	expected, err := service.buildScratchComposerJSON("/project")
	require.NoError(t, err)
	assert.JSONEq(t, string(expected), string(content))

	lockExists, err := afero.Exists(fs, service.tempDir+"/composer.lock")
	require.NoError(t, err)
	assert.False(t, lockExists)

	vendorExists, err := afero.DirExists(fs, service.tempDir+"/vendor")
	require.NoError(t, err)
	assert.False(t, vendorExists)
}

func TestCleanupResetsInitOnceForReinitialization(t *testing.T) {
	fs := afero.NewMemMapFs()
	service := &CLI{logger: zap.NewNop(), fs: fs}

	service.initOnce.Do(service.initTempDir)
	require.NoError(t, service.initErr)
	firstDir := service.tempDir
	require.NotEmpty(t, firstDir)

	service.Cleanup()
	assert.Empty(t, service.tempDir)

	// A later check must reinitialize a fresh scratch project, not silently skip past
	// initTempDir with initOnce already spent and an empty tempDir left over from Cleanup.
	service.initOnce.Do(service.initTempDir)
	require.NoError(t, service.initErr)
	assert.NotEmpty(t, service.tempDir)
	assert.NotEqual(t, firstDir, service.tempDir)

	exists, err := afero.DirExists(fs, service.tempDir)
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestInstallReturnsNilOnSuccess(t *testing.T) {
	stubComposerOutput(t, "Nothing to install")

	service := &CLI{logger: zap.NewNop()}
	require.NoError(t, service.Install(t.Context(), "/tmp"))
}

func TestGetLockHashErrors(t *testing.T) {
	t.Run("missing composer.lock", func(t *testing.T) {
		service := &CLI{logger: zap.NewNop(), fs: afero.NewMemMapFs()}
		_, err := service.GetLockHash("/nowhere")
		require.Error(t, err)
	})

	t.Run("malformed composer.lock", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		require.NoError(t, afero.WriteFile(fs, "/tmp/composer.lock", []byte("not json"), 0o644))

		service := &CLI{logger: zap.NewNop(), fs: fs}
		_, err := service.GetLockHash("/tmp")
		require.Error(t, err)
	})
}

func TestGetInstalledPackageVersionErrors(t *testing.T) {
	t.Run("composer failure", func(t *testing.T) {
		stubComposerFailure(t)
		service := &CLI{logger: zap.NewNop()}
		_, err := service.GetInstalledPackageVersion(t.Context(), "/tmp", "drupal/core")
		require.Error(t, err)
	})

	t.Run("malformed JSON", func(t *testing.T) {
		stubComposerOutput(t, "not-json")
		service := &CLI{logger: zap.NewNop()}
		_, err := service.GetInstalledPackageVersion(t.Context(), "/tmp", "drupal/core")
		require.Error(t, err)
	})

	t.Run("no versions reported", func(t *testing.T) {
		stubComposerOutput(t, `{"versions":[]}`)
		service := &CLI{logger: zap.NewNop()}
		_, err := service.GetInstalledPackageVersion(t.Context(), "/tmp", "drupal/core")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no versions found")
	})
}

func TestDiffFailure(t *testing.T) {
	stubComposerFailure(t)

	service := &CLI{logger: zap.NewNop()}
	entries, err := service.Diff(t.Context(), "/tmp")
	require.Error(t, err)
	assert.Nil(t, entries)
}

func TestGetInstalledPluginsFailure(t *testing.T) {
	stubComposerFailure(t)

	service := &CLI{logger: zap.NewNop()}
	_, err := service.GetInstalledPlugins(t.Context(), "/tmp")
	require.Error(t, err)
}

func TestAuditMalformedOutput(t *testing.T) {
	stubComposerOutput(t, "not-json")

	service := &CLI{logger: zap.NewNop()}
	_, err := service.Audit(t.Context(), "/tmp")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse composer audit output")
}

func TestAuditUnmarshalShapes(t *testing.T) {
	t.Run("no advisories key yields an empty report", func(t *testing.T) {
		var a Audit
		require.NoError(t, a.UnmarshalJSON([]byte(`{"abandoned":{}}`)))
		assert.Empty(t, a.Advisories)
	})

	t.Run("top-level JSON must be an object", func(t *testing.T) {
		var a Audit
		require.Error(t, a.UnmarshalJSON([]byte(`[]`)))
	})

	t.Run("a malformed advisory entry is an error", func(t *testing.T) {
		var a Audit
		require.Error(t, a.UnmarshalJSON([]byte(`{"advisories":{"drupal/core":["not-an-object"]}}`)))
	})

	t.Run("a malformed nested advisory entry is an error", func(t *testing.T) {
		var a Audit
		require.Error(t, a.UnmarshalJSON([]byte(`{"advisories":{"drupal/core":{"a":"not-an-object"}}}`)))
	})

	t.Run("an advisories value of an unexpected type is ignored", func(t *testing.T) {
		var a Audit
		require.NoError(t, a.UnmarshalJSON([]byte(`{"advisories":{"drupal/core":"unexpected"}}`)))
		assert.Empty(t, a.Advisories)
	})

	t.Run("abandoned packages are captured with their suggested replacement", func(t *testing.T) {
		var a Audit
		require.NoError(t, a.UnmarshalJSON([]byte(
			`{"abandoned":{"swiftmailer/swiftmailer":"symfony/mailer","patchwork/jsqueeze":null}}`)))
		assert.Equal(t, []AbandonedPackage{
			{PackageName: "patchwork/jsqueeze"},
			{PackageName: "swiftmailer/swiftmailer", Replacement: "symfony/mailer"},
		}, a.Abandoned)
	})

	t.Run("an empty abandoned array yields no abandoned packages", func(t *testing.T) {
		// The shape composer emits both when nothing is abandoned and when the project set
		// `audit.abandoned: ignore` — it must read as "none", not as a parse failure.
		var a Audit
		require.NoError(t, a.UnmarshalJSON([]byte(`{"abandoned":[]}`)))
		assert.Empty(t, a.Abandoned)
	})

	t.Run("no abandoned key yields no abandoned packages", func(t *testing.T) {
		var a Audit
		require.NoError(t, a.UnmarshalJSON([]byte(`{"advisories":{}}`)))
		assert.Empty(t, a.Abandoned)
	})

	t.Run("an abandoned value of an unexpected type means no replacement", func(t *testing.T) {
		var a Audit
		require.NoError(t, a.UnmarshalJSON([]byte(`{"abandoned":{"foo/bar":42}}`)))
		assert.Equal(t, []AbandonedPackage{{PackageName: "foo/bar"}}, a.Abandoned)
	})

	t.Run("advisories are ordered by package, then advisory id", func(t *testing.T) {
		// Ranging a Go map is random, so without the sort one audit output renders a different
		// report, a different description and a different `composer update` argument list on
		// every run. Named here as well as in the fuzz target: the mutation gate is blocking
		// and must not depend on the fuzzer happening to generate two packages.
		var a Audit
		require.NoError(t, a.UnmarshalJSON([]byte(
			`{"advisories":{"z/one":[{"advisoryId":"Z1","packageName":"z/one"}],`+
				`"a/two":[{"advisoryId":"A2","packageName":"a/two"},{"advisoryId":"A1","packageName":"a/two"}]}}`)))

		ids := make([]string, 0, len(a.Advisories))
		for _, advisory := range a.Advisories {
			ids = append(ids, advisory.AdvisoryID)
		}
		assert.Equal(t, []string{"A1", "A2", "Z1"}, ids)
	})

	t.Run("advisories and abandoned are captured from the same payload", func(t *testing.T) {
		// The parser used to stop at the advisories key; a payload carrying both has to yield
		// both.
		var a Audit
		require.NoError(t, a.UnmarshalJSON([]byte(
			`{"advisories":{"twig/twig":[{"advisoryId":"PKSA-1"}]},"abandoned":{"foo/bar":"baz/qux"}}`)))
		assert.Len(t, a.Advisories, 1)
		assert.Len(t, a.Abandoned, 1)
	})
}

func TestCheckIfPatchAppliesInitError(t *testing.T) {
	// A read-only filesystem makes the scratch project impossible to create; the error must
	// surface rather than being reported as "the patch does not apply".
	service := &CLI{logger: zap.NewNop(), fs: afero.NewReadOnlyFs(afero.NewMemMapFs())}

	_, err := service.CheckIfPatchApplies(t.Context(), "/project", "drupal/core", "10.1.0", "/patches/a.diff")
	require.Error(t, err)
}

func TestCheckIfPatchesApplyInitError(t *testing.T) {
	service := &CLI{logger: zap.NewNop(), fs: afero.NewReadOnlyFs(afero.NewMemMapFs())}

	_, err := service.CheckIfPatchesApply(t.Context(), "/project", "drupal/core", "10.1.0", []string{"/patches/a.diff"})
	require.Error(t, err)
}

func TestGetDependencyPatchesErrors(t *testing.T) {
	t.Run("missing composer.lock", func(t *testing.T) {
		service := &CLI{logger: zap.NewNop(), fs: afero.NewMemMapFs()}
		_, err := service.GetDependencyPatches(t.Context(), "/nowhere")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to read composer.lock")
	})

	t.Run("malformed composer.lock", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		require.NoError(t, afero.WriteFile(fs, "/tmp/composer.lock", []byte("not json"), 0o644))

		service := &CLI{logger: zap.NewNop(), fs: fs}
		_, err := service.GetDependencyPatches(t.Context(), "/tmp")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to unmarshal composer.lock")
	})
}
