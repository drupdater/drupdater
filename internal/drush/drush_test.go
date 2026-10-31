package drush

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

func TestExecDrush(t *testing.T) {
	logger := zaptest.NewLogger(t)
	executor := NewCLI(logger, "/opt/include")

	t.Run("successful execution", func(t *testing.T) {
		// execDrush builds cmd.Env from os.Environ(), so control vars must be
		// in the real process environment via t.Setenv.
		t.Setenv("GO_WANT_HELPER_PROCESS", "1")
		t.Setenv("GOCOVERDIR", "/tmp")
		executor.newCommand = func(ctx context.Context, name string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--", name}
			cs = append(cs, arg...)
			return exec.CommandContext(ctx, os.Args[0], cs...)
		}

		output, err := executor.execDrush(t.Context(), "/tmp", "test_site", "status")
		require.NoError(t, err)
		assert.Equal(t, "[composer exec -- drush status]", output)
	})

	t.Run("SITE_NAME override wins over inherited env", func(t *testing.T) {
		// Ensure that even when SITE_NAME is already present in the process
		// environment, our value is the last (and therefore winning) entry.
		t.Setenv("GO_WANT_HELPER_PROCESS", "1")
		t.Setenv("GOCOVERDIR", "/tmp")
		t.Setenv("SITE_NAME", "inherited_site")

		// Capture the *exec.Cmd that execDrush builds so we can inspect its
		// Env slice after execDrush has mutated it.
		var cmdRef *exec.Cmd
		executor.newCommand = func(ctx context.Context, name string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--", name}
			cs = append(cs, arg...)
			cmd := exec.CommandContext(ctx, os.Args[0], cs...)
			cmdRef = cmd
			return cmd
		}

		_, err := executor.execDrush(t.Context(), "/tmp", "expected_site", "status")
		require.NoError(t, err)

		// The last element must be our intended SITE_NAME, not the inherited one.
		assert.NotNil(t, cmdRef)
		last := cmdRef.Env[len(cmdRef.Env)-1]
		assert.Equal(t, "SITE_NAME=expected_site", last, "SITE_NAME must be the last env entry so it wins over any inherited value")
	})

	t.Run("execution failure", func(t *testing.T) {
		t.Setenv("GO_WANT_HELPER_PROCESS", "1")
		t.Setenv("GO_HELPER_PROCESS_ERROR", "1")
		t.Setenv("GOCOVERDIR", "/tmp")
		executor.newCommand = func(ctx context.Context, name string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--", name}
			cs = append(cs, arg...)
			return exec.CommandContext(ctx, os.Args[0], cs...)
		}

		output, err := executor.execDrush(t.Context(), "/tmp", "test_site", "status")
		require.Error(t, err)
		assert.Empty(t, output)
	})
}

func TestInstallSite(t *testing.T) {
	logger := zap.NewNop()
	cli := NewCLI(logger, "/opt/include")

	t.Run("success", func(t *testing.T) {
		t.Setenv("GO_WANT_HELPER_PROCESS", "1")
		t.Setenv("GO_HELPER_PROCESS_RAW", "1")
		cli.newCommand = func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--", "ok"}
			cs = append(cs, arg...)
			return exec.CommandContext(ctx, os.Args[0], cs...)
		}
		require.NoError(t, cli.InstallSite(t.Context(), "/tmp", "default"))
	})

	t.Run("error", func(t *testing.T) {
		t.Setenv("GO_WANT_HELPER_PROCESS", "1")
		t.Setenv("GO_HELPER_PROCESS_ERROR", "1")
		cli.newCommand = func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--"}
			cs = append(cs, arg...)
			return exec.CommandContext(ctx, os.Args[0], cs...)
		}
		err := cli.InstallSite(t.Context(), "/tmp", "default")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "default")
	})
}

func TestExportConfiguration(t *testing.T) {
	logger := zap.NewNop()
	cli := NewCLI(logger, "/opt/include")

	t.Run("success", func(t *testing.T) {
		t.Setenv("GO_WANT_HELPER_PROCESS", "1")
		t.Setenv("GO_HELPER_PROCESS_RAW", "1")
		var args []string
		cli.newCommand = func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
			args = arg
			cs := []string{"-test.run=TestHelperProcess", "--", "ok"}
			cs = append(cs, arg...)
			return exec.CommandContext(ctx, os.Args[0], cs...)
		}
		require.NoError(t, cli.ExportConfiguration(t.Context(), "/tmp", "site1"))
		// No --commit: the runner commits the export, so Drush never runs git in the repository.
		assert.Equal(t, []string{"config:export", "--yes"}, args[len(args)-2:])
		assert.NotContains(t, args, "--commit")
	})

	t.Run("error", func(t *testing.T) {
		t.Setenv("GO_WANT_HELPER_PROCESS", "1")
		t.Setenv("GO_HELPER_PROCESS_ERROR", "1")
		cli.newCommand = func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--"}
			cs = append(cs, arg...)
			return exec.CommandContext(ctx, os.Args[0], cs...)
		}
		require.Error(t, cli.ExportConfiguration(t.Context(), "/tmp", "site1"))
	})
}

func TestUpdateSite(t *testing.T) {
	logger := zap.NewNop()
	cli := NewCLI(logger, "/opt/include")

	t.Run("success", func(t *testing.T) {
		t.Setenv("GO_WANT_HELPER_PROCESS", "1")
		t.Setenv("GO_HELPER_PROCESS_RAW", "1")
		cli.newCommand = func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--", "ok"}
			cs = append(cs, arg...)
			return exec.CommandContext(ctx, os.Args[0], cs...)
		}
		require.NoError(t, cli.UpdateSite(t.Context(), "/tmp", "site1"))
	})

	t.Run("error", func(t *testing.T) {
		t.Setenv("GO_WANT_HELPER_PROCESS", "1")
		t.Setenv("GO_HELPER_PROCESS_ERROR", "1")
		cli.newCommand = func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--"}
			cs = append(cs, arg...)
			return exec.CommandContext(ctx, os.Args[0], cs...)
		}
		require.Error(t, cli.UpdateSite(t.Context(), "/tmp", "site1"))
	})
}

func TestConfigResave(t *testing.T) {
	logger := zap.NewNop()
	cli := NewCLI(logger, "/opt/include")

	t.Run("success", func(t *testing.T) {
		t.Setenv("GO_WANT_HELPER_PROCESS", "1")
		t.Setenv("GO_HELPER_PROCESS_RAW", "1")
		cli.newCommand = func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--", "ok"}
			cs = append(cs, arg...)
			return exec.CommandContext(ctx, os.Args[0], cs...)
		}
		require.NoError(t, cli.ConfigResave(t.Context(), "/tmp", "site1"))
	})

	t.Run("error", func(t *testing.T) {
		t.Setenv("GO_WANT_HELPER_PROCESS", "1")
		t.Setenv("GO_HELPER_PROCESS_ERROR", "1")
		cli.newCommand = func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--"}
			cs = append(cs, arg...)
			return exec.CommandContext(ctx, os.Args[0], cs...)
		}
		require.Error(t, cli.ConfigResave(t.Context(), "/tmp", "site1"))
	})
}

func TestLocalizeTranslations(t *testing.T) {
	logger := zap.NewNop()
	cli := NewCLI(logger, "/opt/include")

	t.Run("success", func(t *testing.T) {
		t.Setenv("GO_WANT_HELPER_PROCESS", "1")
		t.Setenv("GO_HELPER_PROCESS_RAW", "1")
		cli.newCommand = func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--", "ok"}
			cs = append(cs, arg...)
			return exec.CommandContext(ctx, os.Args[0], cs...)
		}
		require.NoError(t, cli.LocalizeTranslations(t.Context(), "/tmp", "site1"))
	})

	t.Run("error", func(t *testing.T) {
		t.Setenv("GO_WANT_HELPER_PROCESS", "1")
		t.Setenv("GO_HELPER_PROCESS_ERROR", "1")
		cli.newCommand = func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--"}
			cs = append(cs, arg...)
			return exec.CommandContext(ctx, os.Args[0], cs...)
		}
		require.Error(t, cli.LocalizeTranslations(t.Context(), "/tmp", "site1"))
	})
}

func TestHelperProcess(*testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	if os.Getenv("GO_HELPER_PROCESS_ERROR") == "1" {
		os.Exit(1)
	}
	if os.Getenv("GO_HELPER_PROCESS_RAW") == "1" {
		if os.Getenv("GO_HELPER_PROCESS_STDERR_NOISE") == "1" {
			fmt.Fprintln(os.Stderr, "Deprecated function some_contrib_module_hook(): ...")
		}
		fmt.Fprintf(os.Stdout, "%v\n", os.Args[3])
		os.Exit(0)
	}
	fmt.Fprintf(os.Stdout, "%v\n", os.Args[3:])
	os.Exit(0)
}

// fakeDrush makes cli's drush print stdout and records the arguments it was called with.
func fakeDrush(t *testing.T, cli *CLI, stdout string) *[]string {
	t.Helper()
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv("GO_HELPER_PROCESS_RAW", "1")
	t.Setenv("GOCOVERDIR", "/tmp")
	args := new([]string)
	cli.newCommand = func(ctx context.Context, name string, arg ...string) *exec.Cmd {
		*args = append([]string{name}, arg...)
		cs := append([]string{"-test.run=TestHelperProcess", "--", stdout}, arg...)
		return exec.CommandContext(ctx, os.Args[0], cs...)
	}
	return args
}

func TestFactsDecodesTheIncludedCommandsOutput(t *testing.T) {
	cli := NewCLI(zap.NewNop(), "/opt/include")
	args := fakeDrush(t, cli, `{
		"config_sync_directory": "/project/config/sync",
		"translation_path": "/project/web/sites/default/files/translations",
		"modules": ["locale_deploy", "system"],
		"update_hooks": {"system_update_10600": {"module": "system", "update_id": 10600, "description": "Equivalent update.", "type": "hook_update_n"}},
		"unsupported_modules": [{"name": "old", "installed_version": "1.0.0", "recommended_version": "None"}],
		"unsupported_modules_error": ""
	}`)

	facts, err := cli.Facts(t.Context(), "/tmp", "site1")

	require.NoError(t, err)
	assert.Equal(t, []string{"composer", "exec", "--", "drush", "--include=/opt/include", "drupdater:facts"}, *args)
	assert.Equal(t, Facts{
		ConfigSyncDirectory: "/project/config/sync",
		TranslationPath:     "/project/web/sites/default/files/translations",
		Modules:             []string{"locale_deploy", "system"},
		UpdateHooks: map[string]UpdateHook{
			"system_update_10600": {Module: "system", UpdateID: float64(10600), Description: "Equivalent update.", Type: "hook_update_n"},
		},
		UnsupportedModules: []UnsupportedModule{{Name: "old", InstalledVersion: "1.0.0", RecommendedVersion: "None"}},
	}, facts)
	assert.True(t, facts.ModuleEnabled("locale_deploy"))
	assert.False(t, facts.ModuleEnabled("locale"))
}

func TestLocalFactsSkipsTheUpdateStatusRequest(t *testing.T) {
	cli := NewCLI(zap.NewNop(), "/opt/include")
	args := fakeDrush(t, cli, `{"config_sync_directory": "/project/config/sync"}`)

	facts, err := cli.LocalFacts(t.Context(), "/tmp", "site1")

	require.NoError(t, err)
	assert.Equal(t, []string{"composer", "exec", "--", "drush", "--include=/opt/include", "drupdater:facts", "--skip-update-status"}, *args)
	assert.Equal(t, "/project/config/sync", facts.ConfigSyncDirectory)
}

func TestFactsKeepsStderrOutOfThePayload(t *testing.T) {
	cli := NewCLI(zap.NewNop(), "/opt/include")
	fakeDrush(t, cli, `{"modules": ["system"]}`)
	t.Setenv("GO_HELPER_PROCESS_STDERR_NOISE", "1")

	facts, err := cli.Facts(t.Context(), "/tmp", "site1")

	require.NoError(t, err)
	assert.Equal(t, []string{"system"}, facts.Modules)
}

func TestFactsFailures(t *testing.T) {
	t.Run("drush fails", func(t *testing.T) {
		cli := NewCLI(zap.NewNop(), "/opt/include")
		fakeDrush(t, cli, "")
		t.Setenv("GO_HELPER_PROCESS_ERROR", "1")

		_, err := cli.Facts(t.Context(), "/tmp", "site1")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "site1")
	})

	t.Run("output is not JSON", func(t *testing.T) {
		cli := NewCLI(zap.NewNop(), "/opt/include")
		fakeDrush(t, cli, "Bootstrap failed.")

		_, err := cli.Facts(t.Context(), "/tmp", "site1")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to decode the JSON output")
	})
}

func TestFactsCloneSharesNothing(t *testing.T) {
	facts := Facts{
		Modules:            []string{"system"},
		UpdateHooks:        map[string]UpdateHook{"a": {Module: "a"}},
		UnsupportedModules: []UnsupportedModule{{Name: "old"}},
	}

	clone := facts.Clone()
	clone.Modules[0] = "changed"
	clone.UpdateHooks["b"] = UpdateHook{}
	clone.UnsupportedModules[0].Name = "changed"

	assert.Equal(t, []string{"system"}, facts.Modules)
	assert.Len(t, facts.UpdateHooks, 1)
	assert.Equal(t, "old", facts.UnsupportedModules[0].Name)
}

func TestGetConfigSyncDir(t *testing.T) {
	t.Run("relative to the Drupal root", func(t *testing.T) {
		cli := NewCLI(zap.NewNop(), "/opt/include")
		args := fakeDrush(t, cli, `{"config-sync": "../config/sync", "root": "/project/web"}`)

		dir, err := cli.GetConfigSyncDir(t.Context(), "/tmp", "site1")

		require.NoError(t, err)
		assert.Equal(t, "/project/config/sync", dir)
		assert.Equal(t, []string{"composer", "exec", "--", "drush", "status", "--fields=config-sync,root", "--format=json"}, *args)
	})

	t.Run("absolute", func(t *testing.T) {
		cli := NewCLI(zap.NewNop(), "/opt/include")
		fakeDrush(t, cli, `{"config-sync": "/config/sync", "root": "/project/web"}`)

		dir, err := cli.GetConfigSyncDir(t.Context(), "/tmp", "site1")

		require.NoError(t, err)
		assert.Equal(t, "/config/sync", dir)
	})

	t.Run("not configured", func(t *testing.T) {
		cli := NewCLI(zap.NewNop(), "/opt/include")
		fakeDrush(t, cli, `{"config-sync": null, "root": "/project/web"}`)

		_, err := cli.GetConfigSyncDir(t.Context(), "/tmp", "site1")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "site site1 has no config sync directory")
	})

	t.Run("drush fails", func(t *testing.T) {
		cli := NewCLI(zap.NewNop(), "/opt/include")
		fakeDrush(t, cli, "")
		t.Setenv("GO_HELPER_PROCESS_ERROR", "1")

		_, err := cli.GetConfigSyncDir(t.Context(), "/tmp", "site1")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "site1")
	})
}

func TestConfigResaveRunsTheIncludedCommand(t *testing.T) {
	cli := NewCLI(zap.NewNop(), "/opt/include")
	args := fakeDrush(t, cli, "ok")

	require.NoError(t, cli.ConfigResave(t.Context(), "/tmp", "site1"))

	assert.Equal(t, []string{"composer", "exec", "--", "drush", "--include=/opt/include", "drupdater:config-resave"}, *args)
}

func TestExecRunsDrushForTheSiteWithTheGivenArguments(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv("GO_HELPER_PROCESS_RAW", "1")
	cli := NewCLI(zap.NewNop(), "/opt/include")
	var args []string
	cli.newCommand = func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
		args = arg
		cs := append([]string{"-test.run=TestHelperProcess", "--", "cache rebuilt"}, arg...)
		return exec.CommandContext(ctx, os.Args[0], cs...)
	}

	out, err := cli.Exec(t.Context(), "/tmp", "site1", "cache:rebuild")
	require.NoError(t, err)
	assert.Equal(t, "cache rebuilt", out)
	assert.Equal(t, []string{"exec", "--", "drush", "cache:rebuild"}, args[len(args)-4:])
}
