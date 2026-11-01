package composer

import (
	"context"
	"os"
	"os/exec"
	"testing"

	"github.com/drupdater/drupdater/internal/php"

	"encoding/json"
	"errors"
	"fmt"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestExecComposer(t *testing.T) {

	// Create an instance of DefaultComposerService
	service := &CLI{
		logger: zap.NewNop(),
	}

	t.Run("successful execution", func(t *testing.T) {
		service.newCommand = func(ctx context.Context, name string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--", name}
			cs = append(cs, arg...)
			cmd := exec.CommandContext(ctx, os.Args[0], cs...)
			cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1", "GOCOVERDIR=/tmp"}
			return cmd
		}

		output, err := service.execComposer(t.Context(), "/tmp", "update")
		require.NoError(t, err)
		assert.Equal(t, "composer", output)
	})

	t.Run("execution failure", func(t *testing.T) {
		service.newCommand = func(ctx context.Context, name string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--", name}
			cs = append(cs, arg...)
			cmd := exec.CommandContext(ctx, os.Args[0], cs...)
			cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1", "GO_HELPER_PROCESS_ERROR=1", "GOCOVERDIR=/tmp"}
			return cmd
		}

		output, err := service.execComposer(t.Context(), "/tmp", "update")
		require.Error(t, err)
		assert.Empty(t, output)
	})

}

func TestVersion(t *testing.T) {
	service := &CLI{logger: zap.NewNop()}

	// The helper writes everything to stdout. Real composer splits the two lines across stdout
	// and stderr, which Version merges anyway, so the parser sees the same thing.
	printing := func(output string, args *[]string) php.CommandFactory {
		return func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
			if args != nil {
				*args = arg
			}
			cs := []string{"-test.run=TestHelperProcess", "--", output}
			cmd := exec.CommandContext(ctx, os.Args[0], cs...)
			cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1", "GOCOVERDIR=/tmp"}
			return cmd
		}
	}

	t.Run("reports both versions", func(t *testing.T) {
		// Prefixed with the root notices every published image provokes: the versions have to
		// be found among them, not on the first line.
		var args []string
		service.newCommand = printing(`Do not run Composer as root/super user! See https://getcomposer.org/root for details
Composer version 2.10.2 2026-07-14 09:12:33
PHP version 8.3.14 (/usr/local/bin/php)
Run the "diagnose" command to get more detailed diagnostics output.`, &args)

		versions, err := service.Version(t.Context())

		require.NoError(t, err)
		assert.Equal(t, Versions{Composer: "2.10.2", PHP: "8.3.14"}, versions)
		assert.Equal(t, []string{"--version", "--no-ansi"}, args)
	})

	t.Run("tolerates a composer too old to report the PHP version", func(t *testing.T) {
		service.newCommand = printing("Composer version 2.4.4 2022-10-27 14:39:29", nil)

		versions, err := service.Version(t.Context())

		require.NoError(t, err)
		assert.Equal(t, Versions{Composer: "2.4.4"}, versions)
	})

	t.Run("errors when the output names no version", func(t *testing.T) {
		service.newCommand = printing("PHP Warning: something went sideways", nil)

		_, err := service.Version(t.Context())

		require.ErrorContains(t, err, "failed to parse composer version")
	})

	t.Run("errors when composer fails", func(t *testing.T) {
		service.newCommand = func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
			cs := append([]string{"-test.run=TestHelperProcess", "--"}, arg...)
			cmd := exec.CommandContext(ctx, os.Args[0], cs...)
			cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1", "GO_HELPER_PROCESS_ERROR=1", "GOCOVERDIR=/tmp"}
			return cmd
		}

		versions, err := service.Version(t.Context())

		require.ErrorContains(t, err, "failed to determine composer version")
		assert.Equal(t, Versions{}, versions)
	})
}

func TestInstall(t *testing.T) {
	service := &CLI{logger: zap.NewNop()}

	t.Run("success", func(t *testing.T) {
		service.newCommand = func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--", "ok"}
			cs = append(cs, arg...)
			cmd := exec.CommandContext(ctx, os.Args[0], cs...)
			cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1", "GOCOVERDIR=/tmp"}
			return cmd
		}
		require.NoError(t, service.Install(t.Context(), "/tmp"))
	})

	t.Run("error", func(t *testing.T) {
		service.newCommand = func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--"}
			cs = append(cs, arg...)
			cmd := exec.CommandContext(ctx, os.Args[0], cs...)
			cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1", "GO_HELPER_PROCESS_ERROR=1", "GOCOVERDIR=/tmp"}
			return cmd
		}
		err := service.Install(t.Context(), "/tmp")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to install dependencies")
	})
}

func TestNormalize(t *testing.T) {
	service := &CLI{logger: zap.NewNop()}

	service.newCommand = func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
		cs := []string{"-test.run=TestHelperProcess", "--", "normalized"}
		cs = append(cs, arg...)
		cmd := exec.CommandContext(ctx, os.Args[0], cs...)
		cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1", "GOCOVERDIR=/tmp"}
		return cmd
	}

	out, err := service.Normalize(t.Context(), "/tmp")
	require.NoError(t, err)
	assert.Equal(t, "normalized", out)
}

// Shaped like composer-diff 2.4's --format=json output, including PHP's [] for an empty list.
const composerDiffJSON = `{
    "packages": {
        "psr\/log": {"name": "psr\/log", "direct": false, "effective": false, "operation": "upgrade",
            "version_base": "1.1.4", "version_target": "2.0.0",
            "compare": "https:\/\/github.com\/php-fig\/log\/compare\/1.1.4...2.0.0",
            "link": "https:\/\/github.com\/php-fig\/log"},
        "drupal\/token": {"name": "drupal\/token", "direct": true, "effective": false, "operation": "install",
            "version_base": null, "version_target": "1.15.0", "compare": null, "link": null}
    },
    "packages-dev": {
        "phpunit\/phpunit": {"name": "phpunit\/phpunit", "direct": true, "effective": false, "operation": "remove",
            "version_base": "10.5.1", "version_target": null, "compare": null, "link": "https:\/\/github.com\/sebastianbergmann\/phpunit"}
    }
}`

func TestDiff(t *testing.T) {
	var args []string
	newCommand := func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
		args = arg
		cs := append([]string{"-test.run=TestHelperProcess", "--", composerDiffJSON}, arg...)
		cmd := exec.CommandContext(ctx, os.Args[0], cs...)
		cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1", "GOCOVERDIR=/tmp"}
		return cmd
	}

	entries, err := (&CLI{newCommand: newCommand, logger: zap.NewNop()}).Diff(t.Context(), "/tmp")

	require.NoError(t, err)
	assert.Equal(t, []string{"diff", "--format=json", "--with-links"}, args)
	assert.Equal(t, []DiffEntry{
		{Package: "drupal/token", Operation: "install", To: "1.15.0", Direct: true},
		{
			Package: "psr/log", Operation: "upgrade", From: "1.1.4", To: "2.0.0",
			CompareURL: "https://github.com/php-fig/log/compare/1.1.4...2.0.0", ProjectURL: "https://github.com/php-fig/log",
		},
		{
			Package: "phpunit/phpunit", Operation: "remove", From: "10.5.1", Dev: true, Direct: true,
			ProjectURL: "https://github.com/sebastianbergmann/phpunit",
		},
	}, entries)
}

func TestDiffWithNothingChanged(t *testing.T) {
	for name, out := range map[string]string{
		"empty lists":    `{"packages": [], "packages-dev": []}`,
		"null lists":     `{"packages": null, "packages-dev": null}`,
		"lists left out": `{}`,
	} {
		t.Run(name, func(t *testing.T) {
			entries, err := (&CLI{newCommand: composerPrinting(out), logger: zap.NewNop()}).Diff(t.Context(), "/tmp")

			require.NoError(t, err)
			assert.Empty(t, entries)
		})
	}
}

func TestDiffRejectsMalformedOutput(t *testing.T) {
	for name, out := range map[string]string{
		"not json":       "Script failed",
		"prod not a map": `{"packages": "x", "packages-dev": []}`,
		"dev not a map":  `{"packages": [], "packages-dev": 1}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := (&CLI{newCommand: composerPrinting(out), logger: zap.NewNop()}).Diff(t.Context(), "/tmp")

			require.ErrorContains(t, err, "failed to parse composer diff output")
			var syntaxErr *json.SyntaxError
			var typeErr *json.UnmarshalTypeError
			assert.True(t, errors.As(err, &syntaxErr) || errors.As(err, &typeErr), "the decode error is wrapped: %v", err)
		})
	}
}

func TestHelperProcess(*testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	if os.Getenv("GO_HELPER_PROCESS_ERROR") == "1" {
		// A failing composer still prints why it failed, and some callers classify on it.
		if out := os.Getenv("GO_HELPER_PROCESS_OUTPUT"); out != "" {
			fmt.Fprintln(os.Stdout, out)
		}
		os.Exit(1)
	}

	// Lets a test assert on what the subprocess actually received, rather than on the Env slice
	// the caller assembled for it.
	if name := os.Getenv("GO_HELPER_PROCESS_PRINT_ENV"); name != "" {
		fmt.Fprintln(os.Stdout, os.Getenv(name))
		os.Exit(0)
	}

	fmt.Fprintf(os.Stdout, "%v\n", os.Args[3])
	os.Exit(0)
}

// composerPrinting is a composer that prints out on stdout and exits 0.
func composerPrinting(out string) php.CommandFactory {
	return func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
		cs := []string{"-test.run=TestHelperProcess", "--", out}
		cs = append(cs, arg...)
		cmd := exec.CommandContext(ctx, os.Args[0], cs...)
		cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1", "GOCOVERDIR=/tmp"}
		return cmd
	}
}

// composerFailing is a composer that exits non-zero.
func composerFailing() php.CommandFactory {
	return func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
		cs := []string{"-test.run=TestHelperProcess", "--"}
		cs = append(cs, arg...)
		cmd := exec.CommandContext(ctx, os.Args[0], cs...)
		cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1", "GO_HELPER_PROCESS_ERROR=1", "GOCOVERDIR=/tmp"}
		return cmd
	}
}

func TestNewCLI(t *testing.T) {
	logger := zap.NewNop()
	cli := NewCLI(logger)

	require.NotNil(t, cli)
	assert.Equal(t, logger, cli.logger)
	assert.NotNil(t, cli.fs)
}

// The patterns behave as in Composer's PluginManager: anchored, case-insensitive, * for anything.

func TestInstallReturnsNilOnSuccess(t *testing.T) {
	service := &CLI{newCommand: composerPrinting("Nothing to install"), logger: zap.NewNop()}
	require.NoError(t, service.Install(t.Context(), "/tmp"))
}

func TestDiffFailure(t *testing.T) {
	service := &CLI{newCommand: composerFailing(), logger: zap.NewNop()}
	entries, err := service.Diff(t.Context(), "/tmp")
	require.Error(t, err)
	assert.Nil(t, entries)
}
