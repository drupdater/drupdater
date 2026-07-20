package drush

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"

	"github.com/maypok86/otter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

func TestExecDrush(t *testing.T) {
	logger := zaptest.NewLogger(t)
	cache, _ := otter.MustBuilder[string, string](100).Build()
	executor := NewCLI(logger, cache)

	t.Run("successful execution", func(t *testing.T) {
		// execDrush builds cmd.Env from os.Environ(), so control vars must be
		// in the real process environment via t.Setenv.
		t.Setenv("GO_WANT_HELPER_PROCESS", "1")
		t.Setenv("GOCOVERDIR", "/tmp")
		execCommand = func(ctx context.Context, name string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--", name}
			cs = append(cs, arg...)
			return exec.CommandContext(ctx, os.Args[0], cs...)
		}
		defer func() { execCommand = exec.CommandContext }()

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
		execCommand = func(ctx context.Context, name string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--", name}
			cs = append(cs, arg...)
			cmd := exec.CommandContext(ctx, os.Args[0], cs...)
			cmdRef = cmd
			return cmd
		}
		defer func() { execCommand = exec.CommandContext }()

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
		execCommand = func(ctx context.Context, name string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--", name}
			cs = append(cs, arg...)
			return exec.CommandContext(ctx, os.Args[0], cs...)
		}
		defer func() { execCommand = exec.CommandContext }()

		output, err := executor.execDrush(t.Context(), "/tmp", "test_site", "status")
		require.Error(t, err)
		assert.Empty(t, output)
	})
}

func TestGetUpdateHooks(t *testing.T) {

	logger := zap.NewNop()

	t.Run("JSON of updates", func(t *testing.T) {
		data := `{
					"ad_entity_update_8007": {
						"module": "ad_entity",
						"update_id": 8007,
						"description": "8007 - Fix ad_entity.settings due to module uninstalls of sub-modules.",
						"type": "hook_update_n"
					},
					"entity_reference_display_update_8001": {
						"module": "entity_reference_display",
						"update_id": "8001",
						"description": "8001 - Updates the \"negate\" field settings option from integer to boolean.",
						"type": "hook_update_n"
					},
					"menu_link_attributes_update_8002": {
						"module": "menu_link_attributes",
						"update_id": 8002,
						"description": "8002 - Add labels and description to default menu item attributes for clarification.",
						"type": "hook_update_n"
					},
					"migrate_tools_update_10000": {
						"module": "migrate_tools",
						"update_id": "10000",
						"description": "10000 - Adds a table in the database dedicated to SyncSourceIds entries.",
						"type": "hook_update_n"
					}
				}`

		t.Setenv("GO_WANT_HELPER_PROCESS", "1")
		t.Setenv("GO_HELPER_PROCESS_RAW", "1")
		t.Setenv("GOCOVERDIR", "/tmp")
		execCommand = func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--", data}
			cs = append(cs, arg...)
			return exec.CommandContext(ctx, os.Args[0], cs...)
		}
		defer func() { execCommand = exec.CommandContext }()

		drush := CLI{
			logger: logger,
		}

		updates, err := drush.GetUpdateHooks(t.Context(), "/tmp", "site1")

		require.NoError(t, err)

		if len(updates) != 4 {
			t.Errorf("Expected 4 updates, got %d", len(updates))
		}

		assert.Equal(t, map[string]UpdateHook{
			"ad_entity_update_8007": {
				Module:      "ad_entity",
				UpdateID:    float64(8007),
				Description: "8007 - Fix ad_entity.settings due to module uninstalls of sub-modules.",
				Type:        "hook_update_n",
			},
			"entity_reference_display_update_8001": {
				Module:      "entity_reference_display",
				UpdateID:    "8001",
				Description: "8001 - Updates the \"negate\" field settings option from integer to boolean.",
				Type:        "hook_update_n",
			},
			"menu_link_attributes_update_8002": {
				Module:      "menu_link_attributes",
				UpdateID:    float64(8002),
				Description: "8002 - Add labels and description to default menu item attributes for clarification.",
				Type:        "hook_update_n",
			},
			"migrate_tools_update_10000": {
				Module:      "migrate_tools",
				UpdateID:    "10000",
				Description: "10000 - Adds a table in the database dedicated to SyncSourceIds entries.",
				Type:        "hook_update_n",
			},
		}, updates)
	})

	t.Run("No updates", func(t *testing.T) {
		data := ` [success] No database updates required.`

		t.Setenv("GO_WANT_HELPER_PROCESS", "1")
		t.Setenv("GOCOVERDIR", "/tmp")
		execCommand = func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--", data}
			cs = append(cs, arg...)
			return exec.CommandContext(ctx, os.Args[0], cs...)
		}
		defer func() { execCommand = exec.CommandContext }()

		drush := CLI{
			logger: logger,
		}

		updates, err := drush.GetUpdateHooks(t.Context(), "/tmp", "site1")

		require.NoError(t, err)

		if len(updates) != 0 {
			t.Errorf("Expected 0 updates, got %d", len(updates))
		}

		assert.Nil(t, updates)
	})

}

func TestGetUnsupportedModules(t *testing.T) {
	logger := zap.NewNop()

	t.Run("JSON of unsupported modules", func(t *testing.T) {
		data := `[{"name":"module_a","installed_version":"1.0.0","recommended_version":"None"},{"name":"module_b","installed_version":"2.3.1","recommended_version":"3.0.0"}]`

		t.Setenv("GO_WANT_HELPER_PROCESS", "1")
		t.Setenv("GO_HELPER_PROCESS_RAW", "1")
		t.Setenv("GOCOVERDIR", "/tmp")
		execCommand = func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--", data}
			cs = append(cs, arg...)
			return exec.CommandContext(ctx, os.Args[0], cs...)
		}
		defer func() { execCommand = exec.CommandContext }()

		drush := CLI{
			logger: logger,
		}

		modules, err := drush.GetUnsupportedModules(t.Context(), "/tmp", "site1")

		require.NoError(t, err)
		assert.Equal(t, []UnsupportedModule{
			{Name: "module_a", InstalledVersion: "1.0.0", RecommendedVersion: "None"},
			{Name: "module_b", InstalledVersion: "2.3.1", RecommendedVersion: "3.0.0"},
		}, modules)
	})

	t.Run("no unsupported modules", func(t *testing.T) {
		data := `[]`

		t.Setenv("GO_WANT_HELPER_PROCESS", "1")
		t.Setenv("GO_HELPER_PROCESS_RAW", "1")
		t.Setenv("GOCOVERDIR", "/tmp")
		execCommand = func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--", data}
			cs = append(cs, arg...)
			return exec.CommandContext(ctx, os.Args[0], cs...)
		}
		defer func() { execCommand = exec.CommandContext }()

		drush := CLI{
			logger: logger,
		}

		modules, err := drush.GetUnsupportedModules(t.Context(), "/tmp", "site1")

		require.NoError(t, err)
		assert.Empty(t, modules)
	})

	t.Run("empty output", func(t *testing.T) {
		data := ``

		t.Setenv("GO_WANT_HELPER_PROCESS", "1")
		t.Setenv("GO_HELPER_PROCESS_RAW", "1")
		t.Setenv("GOCOVERDIR", "/tmp")
		execCommand = func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--", data}
			cs = append(cs, arg...)
			return exec.CommandContext(ctx, os.Args[0], cs...)
		}
		defer func() { execCommand = exec.CommandContext }()

		drush := CLI{
			logger: logger,
		}

		modules, err := drush.GetUnsupportedModules(t.Context(), "/tmp", "site1")

		require.NoError(t, err)
		assert.Nil(t, modules)
	})

	t.Run("execution failure", func(t *testing.T) {
		t.Setenv("GO_WANT_HELPER_PROCESS", "1")
		t.Setenv("GO_HELPER_PROCESS_ERROR", "1")
		t.Setenv("GOCOVERDIR", "/tmp")
		execCommand = func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--"}
			cs = append(cs, arg...)
			return exec.CommandContext(ctx, os.Args[0], cs...)
		}
		defer func() { execCommand = exec.CommandContext }()

		drush := CLI{
			logger: logger,
		}

		modules, err := drush.GetUnsupportedModules(t.Context(), "/tmp", "site1")

		require.Error(t, err)
		assert.Nil(t, modules)
	})

	t.Run("invalid JSON", func(t *testing.T) {
		data := `not json`

		t.Setenv("GO_WANT_HELPER_PROCESS", "1")
		t.Setenv("GO_HELPER_PROCESS_RAW", "1")
		t.Setenv("GOCOVERDIR", "/tmp")
		execCommand = func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--", data}
			cs = append(cs, arg...)
			return exec.CommandContext(ctx, os.Args[0], cs...)
		}
		defer func() { execCommand = exec.CommandContext }()

		drush := CLI{
			logger: logger,
		}

		modules, err := drush.GetUnsupportedModules(t.Context(), "/tmp", "site1")

		require.Error(t, err)
		assert.Nil(t, modules)
	})
}

func TestInstallSite(t *testing.T) {
	logger := zap.NewNop()
	cache, _ := otter.MustBuilder[string, string](100).Build()
	cli := NewCLI(logger, cache)

	t.Run("success", func(t *testing.T) {
		t.Setenv("GO_WANT_HELPER_PROCESS", "1")
		t.Setenv("GO_HELPER_PROCESS_RAW", "1")
		execCommand = func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--", "ok"}
			cs = append(cs, arg...)
			return exec.CommandContext(ctx, os.Args[0], cs...)
		}
		defer func() { execCommand = exec.CommandContext }()
		require.NoError(t, cli.InstallSite(t.Context(), "/tmp", "default"))
	})

	t.Run("error", func(t *testing.T) {
		t.Setenv("GO_WANT_HELPER_PROCESS", "1")
		t.Setenv("GO_HELPER_PROCESS_ERROR", "1")
		execCommand = func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--"}
			cs = append(cs, arg...)
			return exec.CommandContext(ctx, os.Args[0], cs...)
		}
		defer func() { execCommand = exec.CommandContext }()
		err := cli.InstallSite(t.Context(), "/tmp", "default")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "default")
	})
}

func TestGetConfigSyncDir(t *testing.T) {
	logger := zap.NewNop()

	t.Run("absolute path", func(t *testing.T) {
		cache, _ := otter.MustBuilder[string, string](100).Build()
		cli := NewCLI(logger, cache)
		t.Setenv("GO_WANT_HELPER_PROCESS", "1")
		t.Setenv("GO_HELPER_PROCESS_RAW", "1")
		execCommand = func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--", "/tmp/config/sync"}
			cs = append(cs, arg...)
			return exec.CommandContext(ctx, os.Args[0], cs...)
		}
		defer func() { execCommand = exec.CommandContext }()
		dir, err := cli.GetConfigSyncDir(t.Context(), "/tmp", "site1", false)
		require.NoError(t, err)
		assert.Equal(t, "/tmp/config/sync", dir)
	})

	t.Run("relative path", func(t *testing.T) {
		cache, _ := otter.MustBuilder[string, string](100).Build()
		cli := NewCLI(logger, cache)
		t.Setenv("GO_WANT_HELPER_PROCESS", "1")
		t.Setenv("GO_HELPER_PROCESS_RAW", "1")
		execCommand = func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--", "/tmp/config/sync"}
			cs = append(cs, arg...)
			return exec.CommandContext(ctx, os.Args[0], cs...)
		}
		defer func() { execCommand = exec.CommandContext }()
		dir, err := cli.GetConfigSyncDir(t.Context(), "/tmp", "site1", true)
		require.NoError(t, err)
		assert.Equal(t, "config/sync", dir)
	})

	t.Run("cache hit", func(t *testing.T) {
		cache, _ := otter.MustBuilder[string, string](100).Build()
		cli := NewCLI(logger, cache)
		t.Setenv("GO_WANT_HELPER_PROCESS", "1")
		t.Setenv("GO_HELPER_PROCESS_RAW", "1")
		callCount := 0
		execCommand = func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
			callCount++
			cs := []string{"-test.run=TestHelperProcess", "--", "/cached/path"}
			cs = append(cs, arg...)
			return exec.CommandContext(ctx, os.Args[0], cs...)
		}
		defer func() { execCommand = exec.CommandContext }()
		v1, _ := cli.GetConfigSyncDir(t.Context(), "/tmp", "site1", false)
		v2, _ := cli.GetConfigSyncDir(t.Context(), "/tmp", "site1", false)
		assert.Equal(t, v1, v2)
		assert.Equal(t, 1, callCount)
	})

	t.Run("error", func(t *testing.T) {
		cache, _ := otter.MustBuilder[string, string](100).Build()
		cli := NewCLI(logger, cache)
		t.Setenv("GO_WANT_HELPER_PROCESS", "1")
		t.Setenv("GO_HELPER_PROCESS_ERROR", "1")
		execCommand = func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--"}
			cs = append(cs, arg...)
			return exec.CommandContext(ctx, os.Args[0], cs...)
		}
		defer func() { execCommand = exec.CommandContext }()
		_, err := cli.GetConfigSyncDir(t.Context(), "/tmp", "site1", false)
		require.Error(t, err)
	})
}

func TestExportConfiguration(t *testing.T) {
	logger := zap.NewNop()
	cache, _ := otter.MustBuilder[string, string](100).Build()
	cli := NewCLI(logger, cache)

	t.Run("success", func(t *testing.T) {
		t.Setenv("GO_WANT_HELPER_PROCESS", "1")
		t.Setenv("GO_HELPER_PROCESS_RAW", "1")
		execCommand = func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--", "ok"}
			cs = append(cs, arg...)
			return exec.CommandContext(ctx, os.Args[0], cs...)
		}
		defer func() { execCommand = exec.CommandContext }()
		require.NoError(t, cli.ExportConfiguration(t.Context(), "/tmp", "site1"))
	})

	t.Run("error", func(t *testing.T) {
		t.Setenv("GO_WANT_HELPER_PROCESS", "1")
		t.Setenv("GO_HELPER_PROCESS_ERROR", "1")
		execCommand = func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--"}
			cs = append(cs, arg...)
			return exec.CommandContext(ctx, os.Args[0], cs...)
		}
		defer func() { execCommand = exec.CommandContext }()
		require.Error(t, cli.ExportConfiguration(t.Context(), "/tmp", "site1"))
	})
}

func TestUpdateSite(t *testing.T) {
	logger := zap.NewNop()
	cache, _ := otter.MustBuilder[string, string](100).Build()
	cli := NewCLI(logger, cache)

	t.Run("success", func(t *testing.T) {
		t.Setenv("GO_WANT_HELPER_PROCESS", "1")
		t.Setenv("GO_HELPER_PROCESS_RAW", "1")
		execCommand = func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--", "ok"}
			cs = append(cs, arg...)
			return exec.CommandContext(ctx, os.Args[0], cs...)
		}
		defer func() { execCommand = exec.CommandContext }()
		require.NoError(t, cli.UpdateSite(t.Context(), "/tmp", "site1"))
	})

	t.Run("error", func(t *testing.T) {
		t.Setenv("GO_WANT_HELPER_PROCESS", "1")
		t.Setenv("GO_HELPER_PROCESS_ERROR", "1")
		execCommand = func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--"}
			cs = append(cs, arg...)
			return exec.CommandContext(ctx, os.Args[0], cs...)
		}
		defer func() { execCommand = exec.CommandContext }()
		require.Error(t, cli.UpdateSite(t.Context(), "/tmp", "site1"))
	})
}

func TestConfigResave(t *testing.T) {
	logger := zap.NewNop()
	cache, _ := otter.MustBuilder[string, string](100).Build()
	cli := NewCLI(logger, cache)

	t.Run("success", func(t *testing.T) {
		t.Setenv("GO_WANT_HELPER_PROCESS", "1")
		t.Setenv("GO_HELPER_PROCESS_RAW", "1")
		execCommand = func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--", "ok"}
			cs = append(cs, arg...)
			return exec.CommandContext(ctx, os.Args[0], cs...)
		}
		defer func() { execCommand = exec.CommandContext }()
		require.NoError(t, cli.ConfigResave(t.Context(), "/tmp", "site1"))
	})

	t.Run("error", func(t *testing.T) {
		t.Setenv("GO_WANT_HELPER_PROCESS", "1")
		t.Setenv("GO_HELPER_PROCESS_ERROR", "1")
		execCommand = func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--"}
			cs = append(cs, arg...)
			return exec.CommandContext(ctx, os.Args[0], cs...)
		}
		defer func() { execCommand = exec.CommandContext }()
		require.Error(t, cli.ConfigResave(t.Context(), "/tmp", "site1"))
	})
}

func TestIsModuleEnabled(t *testing.T) {
	logger := zap.NewNop()
	cache, _ := otter.MustBuilder[string, string](100).Build()
	cli := NewCLI(logger, cache)

	t.Run("enabled", func(t *testing.T) {
		t.Setenv("GO_WANT_HELPER_PROCESS", "1")
		t.Setenv("GO_HELPER_PROCESS_RAW", "1")
		execCommand = func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--", "mymodule"}
			cs = append(cs, arg...)
			return exec.CommandContext(ctx, os.Args[0], cs...)
		}
		defer func() { execCommand = exec.CommandContext }()
		enabled, err := cli.IsModuleEnabled(t.Context(), "/tmp", "site1", "mymodule")
		require.NoError(t, err)
		assert.True(t, enabled)
	})

	t.Run("not enabled", func(t *testing.T) {
		t.Setenv("GO_WANT_HELPER_PROCESS", "1")
		t.Setenv("GO_HELPER_PROCESS_RAW", "1")
		execCommand = func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--", ""}
			cs = append(cs, arg...)
			return exec.CommandContext(ctx, os.Args[0], cs...)
		}
		defer func() { execCommand = exec.CommandContext }()
		enabled, err := cli.IsModuleEnabled(t.Context(), "/tmp", "site1", "mymodule")
		require.NoError(t, err)
		assert.False(t, enabled)
	})
}

func TestLocalizeTranslations(t *testing.T) {
	logger := zap.NewNop()
	cache, _ := otter.MustBuilder[string, string](100).Build()
	cli := NewCLI(logger, cache)

	t.Run("success", func(t *testing.T) {
		t.Setenv("GO_WANT_HELPER_PROCESS", "1")
		t.Setenv("GO_HELPER_PROCESS_RAW", "1")
		execCommand = func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--", "ok"}
			cs = append(cs, arg...)
			return exec.CommandContext(ctx, os.Args[0], cs...)
		}
		defer func() { execCommand = exec.CommandContext }()
		require.NoError(t, cli.LocalizeTranslations(t.Context(), "/tmp", "site1"))
	})

	t.Run("error", func(t *testing.T) {
		t.Setenv("GO_WANT_HELPER_PROCESS", "1")
		t.Setenv("GO_HELPER_PROCESS_ERROR", "1")
		execCommand = func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--"}
			cs = append(cs, arg...)
			return exec.CommandContext(ctx, os.Args[0], cs...)
		}
		defer func() { execCommand = exec.CommandContext }()
		require.Error(t, cli.LocalizeTranslations(t.Context(), "/tmp", "site1"))
	})
}

func TestGetTranslationPath(t *testing.T) {
	logger := zap.NewNop()

	t.Run("absolute path", func(t *testing.T) {
		cache, _ := otter.MustBuilder[string, string](100).Build()
		cli := NewCLI(logger, cache)
		t.Setenv("GO_WANT_HELPER_PROCESS", "1")
		t.Setenv("GO_HELPER_PROCESS_RAW", "1")
		execCommand = func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--", "/tmp/translations"}
			cs = append(cs, arg...)
			return exec.CommandContext(ctx, os.Args[0], cs...)
		}
		defer func() { execCommand = exec.CommandContext }()
		path, err := cli.GetTranslationPath(t.Context(), "/tmp", "site1", false)
		require.NoError(t, err)
		assert.Equal(t, "/tmp/translations", path)
	})

	t.Run("relative path", func(t *testing.T) {
		cache, _ := otter.MustBuilder[string, string](100).Build()
		cli := NewCLI(logger, cache)
		t.Setenv("GO_WANT_HELPER_PROCESS", "1")
		t.Setenv("GO_HELPER_PROCESS_RAW", "1")
		execCommand = func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--", "/tmp/translations"}
			cs = append(cs, arg...)
			return exec.CommandContext(ctx, os.Args[0], cs...)
		}
		defer func() { execCommand = exec.CommandContext }()
		path, err := cli.GetTranslationPath(t.Context(), "/tmp", "site1", true)
		require.NoError(t, err)
		assert.Equal(t, "translations", path)
	})

	t.Run("cache hit", func(t *testing.T) {
		cache, _ := otter.MustBuilder[string, string](100).Build()
		cli := NewCLI(logger, cache)
		t.Setenv("GO_WANT_HELPER_PROCESS", "1")
		t.Setenv("GO_HELPER_PROCESS_RAW", "1")
		callCount := 0
		execCommand = func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
			callCount++
			cs := []string{"-test.run=TestHelperProcess", "--", "/cached/translations"}
			cs = append(cs, arg...)
			return exec.CommandContext(ctx, os.Args[0], cs...)
		}
		defer func() { execCommand = exec.CommandContext }()
		v1, _ := cli.GetTranslationPath(t.Context(), "/tmp", "site1", false)
		v2, _ := cli.GetTranslationPath(t.Context(), "/tmp", "site1", false)
		assert.Equal(t, v1, v2)
		assert.Equal(t, 1, callCount)
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
		fmt.Fprintf(os.Stdout, "%v\n", os.Args[3])
		os.Exit(0)
	}
	fmt.Fprintf(os.Stdout, "%v\n", os.Args[3:])
	os.Exit(0)
}
