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

	"github.com/spf13/afero"
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

func TestGetInstalledPlugins(t *testing.T) {

	t.Run("A list of plugins", func(t *testing.T) {

		data := `Package "composer-plugin-api *" found in version "2.6.0".
composer/installers                            v2.3.0  requires composer-plugin-api (^1.0 || ^2.0)
cweagans/composer-patches                      1.7.3   requires composer-plugin-api (^1.0 || ^2.0)
dealerdirect/phpcodesniffer-composer-installer v1.0.0  requires composer-plugin-api (^1.0 || ^2.0)
drupal/core-composer-scaffold                  10.4.1  requires composer-plugin-api (^2)
oomphinc/composer-installers-extender          2.0.1   requires composer-plugin-api (^1.1 || ^2.0)
php-http/discovery                             1.20.0  requires composer-plugin-api (^1.0|^2.0)
phpro/grumphp-shim                             v2.10.0 requires composer-plugin-api (~2.0)
phpstan/extension-installer                    1.4.3   requires composer-plugin-api (^2.0)
tbachert/spi                                   v1.0.2  requires composer-plugin-api (^2.0)
zaporylie/composer-drupal-optimizations        1.2.0   requires composer-plugin-api (^1.1 || ^2.0)`

		newCommand := func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--", data}
			cs = append(cs, arg...)
			cmd := exec.CommandContext(ctx, os.Args[0], cs...)
			cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1", "GOCOVERDIR=/tmp"}
			return cmd
		}

		service := &CLI{
			newCommand: newCommand,
			logger:     zap.NewNop(),
		}

		plugins, err := service.GetInstalledPlugins(t.Context(), "/tmp")

		require.NoError(t, err)
		assert.Equal(t, map[string]any{
			"composer/installers":                            nil,
			"cweagans/composer-patches":                      nil,
			"dealerdirect/phpcodesniffer-composer-installer": nil,
			"drupal/core-composer-scaffold":                  nil,
			"oomphinc/composer-installers-extender":          nil,
			"php-http/discovery":                             nil,
			"phpro/grumphp-shim":                             nil,
			"phpstan/extension-installer":                    nil,
			"tbachert/spi":                                   nil,
			"zaporylie/composer-drupal-optimizations":        nil,
		}, plugins)
	})

}

func TestRunComposerAudit(t *testing.T) {

	t.Run("No vulnerabilities", func(t *testing.T) {

		data := `{
    "advisories": [],
    "abandoned": []
}`

		service := &CLI{
			logger: zap.NewNop(),
		}

		service.newCommand = func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--", data}
			cs = append(cs, arg...)
			cmd := exec.CommandContext(ctx, os.Args[0], cs...)
			cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1", "GOCOVERDIR=/tmp"}
			return cmd
		}

		audit, err := service.Audit(t.Context(), "/tmp")

		require.NoError(t, err)
		assert.Nil(t, audit.Advisories)
		// composer emits an empty JSON array rather than an empty object when it has no
		// abandoned package to report.
		assert.Nil(t, audit.Abandoned)
	})

	t.Run("Some vulnerabilities", func(t *testing.T) {

		data := `{
	   	    "advisories": {
	   	        "twig/twig": [
	   	            {
	   	                "advisoryId": "PKSA-v3kg-5xkr-pykw",
	   	                "packageName": "twig/twig",
	   	                "affectedVersions": ">=3.16.0,<3.19.0",
	   	                "title": "Missing output escaping for the null coalesce operator",
	   	                "cve": "CVE-2025-24374",
	   	                "link": "https://symfony.com/blog/twig-cve-2025-24374-missing-output-escaping-for-the-null-coalesce-operator",
	   	                "reportedAt": "2025-01-29T06:52:00+00:00",
	   	                "sources": [
	   	                    {
	   	                        "name": "GitHub",
	   	                        "remoteId": "GHSA-3xg3-cgvq-2xwr"
	   	                    },
	   	                    {
	   	                        "name": "FriendsOfPHP/security-advisories",
	   	                        "remoteId": "twig/twig/CVE-2025-24374.yaml"
	   	                    }
	   	                ],
	   	                "severity": "medium"
	   	            }
	   	        ]
	   	    },
	   	    "abandoned": {
	   	        "swiftmailer/swiftmailer": "symfony/mailer",
	   	        "j7mbo/twitter-api-php": null
	   	    }
	   	}`

		newCommand := func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--", data}
			cs = append(cs, arg...)
			cmd := exec.CommandContext(ctx, os.Args[0], cs...)
			cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1", "GOCOVERDIR=/tmp"}
			return cmd
		}

		service := &CLI{
			newCommand: newCommand,
			logger:     zap.NewNop(),
		}

		audit, err := service.Audit(t.Context(), "/tmp")

		require.NoError(t, err)

		assert.Len(t, audit.Advisories, 1)
		assert.Equal(t, []Advisory{
			{
				ReportedAt:       "2025-01-29T06:52:00+00:00",
				Severity:         "medium",
				AdvisoryID:       "PKSA-v3kg-5xkr-pykw",
				CVE:              "CVE-2025-24374",
				Sources:          []Source{{Name: "GitHub", RemoteID: "GHSA-3xg3-cgvq-2xwr"}, {Name: "FriendsOfPHP/security-advisories", RemoteID: "twig/twig/CVE-2025-24374.yaml"}},
				Link:             "https://symfony.com/blog/twig-cve-2025-24374-missing-output-escaping-for-the-null-coalesce-operator",
				PackageName:      "twig/twig",
				AffectedVersions: ">=3.16.0,<3.19.0",
				Title:            "Missing output escaping for the null coalesce operator",
			},
		}, audit.Advisories)

		// Sorted by package name, not in composer's declaration order, so the report and the
		// merge request description are byte-identical for unchanged input.
		assert.Equal(t, []AbandonedPackage{
			{PackageName: "j7mbo/twitter-api-php"},
			{PackageName: "swiftmailer/swiftmailer", Replacement: "symfony/mailer"},
		}, audit.Abandoned)
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

func TestGetComposerLockHash(t *testing.T) {
	data := `{
	   		"content-hash": "d3d29b1f6a1d8f2c3b9b8e1e4f5f9e3e"
	   	}`

	fs := afero.NewMemMapFs()
	err := afero.WriteFile(fs, "/test/composer.lock", []byte(data), 0644)
	require.NoError(t, err)

	service := &CLI{
		logger: zap.NewNop(),
		fs:     fs,
	}
	hash, err := service.GetLockHash("/test")

	require.NoError(t, err)
	assert.Equal(t, "d3d29b1f6a1d8f2c3b9b8e1e4f5f9e3e", hash)
}

func TestGetInstalledPackageVersion(t *testing.T) {

	service := &CLI{
		logger: zap.NewNop(),
	}

	t.Run("returns version when versions array is non-empty", func(t *testing.T) {
		data := `{"versions":["1.2.3"]}`

		service.newCommand = func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--", data}
			cs = append(cs, arg...)
			cmd := exec.CommandContext(ctx, os.Args[0], cs...)
			cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1", "GOCOVERDIR=/tmp"}
			return cmd
		}

		version, err := service.GetInstalledPackageVersion(t.Context(), "/tmp", "drupal/core")
		require.NoError(t, err)
		assert.Equal(t, "1.2.3", version)
	})

	t.Run("returns error when versions array is empty", func(t *testing.T) {
		data := `{"versions":[]}`

		service.newCommand = func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--", data}
			cs = append(cs, arg...)
			cmd := exec.CommandContext(ctx, os.Args[0], cs...)
			cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1", "GOCOVERDIR=/tmp"}
			return cmd
		}

		version, err := service.GetInstalledPackageVersion(t.Context(), "/tmp", "drupal/core")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no versions found for package drupal/core")
		assert.Empty(t, version)
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

func TestCheckPlatformReqs(t *testing.T) {
	service := &CLI{logger: zap.NewNop()}

	t.Run("requirements satisfied", func(t *testing.T) {
		json := `[{"name":"php","version":"8.3.0","status":"success","failed_requirement":null,"provider":null}]`
		service.newCommand = func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--", json}
			cs = append(cs, arg...)
			cmd := exec.CommandContext(ctx, os.Args[0], cs...)
			cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1", "GOCOVERDIR=/tmp"}
			return cmd
		}

		out, err := service.CheckPlatformReqs(t.Context(), "/tmp")
		require.NoError(t, err)
		assert.Empty(t, out)
	})

	t.Run("output prefixed with composer's informational line", func(t *testing.T) {
		json := "Checking platform requirements using the lock file\n" +
			`[` +
			`{"name":"php","version":"8.4.23","status":"success","failed_requirement":null,"provider":null},` +
			`{"name":"ext-igbinary","version":"n/a","status":"missing","failed_requirement":{"source":"drupal/igbinary","type":"requires","target":"ext-igbinary","constraint":"*"},"provider":null}` +
			`]`
		service.newCommand = func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--", json}
			cs = append(cs, arg...)
			cmd := exec.CommandContext(ctx, os.Args[0], cs...)
			cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1", "GOCOVERDIR=/tmp"}
			return cmd
		}

		out, err := service.CheckPlatformReqs(t.Context(), "/tmp")
		require.NoError(t, err)
		assert.Empty(t, out)
	})

	t.Run("output with trailing noise after the array", func(t *testing.T) {
		// Merged output interleaves text on both sides of the payload, so the span runs to the
		// last closing bracket. Known limit: trailing text with its own brackets would extend
		// it past the payload — composer does not emit that today.
		json := "Checking platform requirements using the lock file\n" +
			`[{"name":"php","version":"8.4.23","status":"success","failed_requirement":null,"provider":null}]` +
			"\nDone in 0.4s"
		service.newCommand = func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--", json}
			cs = append(cs, arg...)
			cmd := exec.CommandContext(ctx, os.Args[0], cs...) //nolint:gosec // test helper process
			cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1", "GOCOVERDIR=/tmp"}
			return cmd
		}

		out, err := service.CheckPlatformReqs(t.Context(), "/tmp")
		require.NoError(t, err)
		assert.Empty(t, out)
	})

	t.Run("output with no JSON array at all", func(t *testing.T) {
		// composer failing before it produces a payload must surface as a parse error naming
		// the real output, not as an empty "everything is fine" result.
		service.newCommand = func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--", "Could not open input file: composer"}
			cs = append(cs, arg...)
			cmd := exec.CommandContext(ctx, os.Args[0], cs...) //nolint:gosec // test helper process
			cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1", "GOCOVERDIR=/tmp"}
			return cmd
		}

		_, err := service.CheckPlatformReqs(t.Context(), "/tmp")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse composer check-platform-reqs output")
		assert.Contains(t, err.Error(), "Could not open input file")
	})

	t.Run("output whose brackets are in the wrong order", func(t *testing.T) {
		// "]" before "[" must not be treated as a payload span; slicing on it would panic or
		// yield nonsense, so the guard requires the closing bracket to come after the opening.
		service.newCommand = func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--", "] not json ["}
			cs = append(cs, arg...)
			cmd := exec.CommandContext(ctx, os.Args[0], cs...) //nolint:gosec // test helper process
			cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1", "GOCOVERDIR=/tmp"}
			return cmd
		}

		_, err := service.CheckPlatformReqs(t.Context(), "/tmp")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse composer check-platform-reqs output")
	})

	t.Run("php requirement not satisfied", func(t *testing.T) {
		json := `[{"name":"php","version":"8.1.0","status":"failed","failed_requirement":{"source":"drupal/core","type":"requires","target":"php","constraint":">=8.3"},"provider":null}]`
		service.newCommand = func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--", json}
			cs = append(cs, arg...)
			cmd := exec.CommandContext(ctx, os.Args[0], cs...)
			cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1", "GOCOVERDIR=/tmp"}
			return cmd
		}

		out, err := service.CheckPlatformReqs(t.Context(), "/tmp")
		require.Error(t, err)
		assert.Contains(t, out, "php")
		assert.Contains(t, out, ">=8.3")
	})

	t.Run("php requirement missing entirely", func(t *testing.T) {
		json := `[{"name":"php-64bit","version":"n/a","status":"missing","failed_requirement":{"source":"drupal/core","type":"requires","target":"php-64bit","constraint":"*"},"provider":null}]`
		service.newCommand = func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--", json}
			cs = append(cs, arg...)
			cmd := exec.CommandContext(ctx, os.Args[0], cs...)
			cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1", "GOCOVERDIR=/tmp"}
			return cmd
		}

		out, err := service.CheckPlatformReqs(t.Context(), "/tmp")
		require.Error(t, err)
		assert.Contains(t, out, "php-64bit")
	})

	t.Run("failed extension requirement is ignored", func(t *testing.T) {
		json := `[
			{"name":"php","version":"8.3.0","status":"success","failed_requirement":null,"provider":null},
			{"name":"ext-gd","version":"n/a","status":"missing","failed_requirement":{"source":"drupal/core","type":"requires","target":"ext-gd","constraint":"*"},"provider":null}
		]`
		service.newCommand = func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--", json}
			cs = append(cs, arg...)
			cmd := exec.CommandContext(ctx, os.Args[0], cs...)
			cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1", "GOCOVERDIR=/tmp"}
			return cmd
		}

		out, err := service.CheckPlatformReqs(t.Context(), "/tmp")
		require.NoError(t, err)
		assert.Empty(t, out)
	})

	t.Run("exec failure with unparsable output", func(t *testing.T) {
		service.newCommand = func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--", "not json"}
			cs = append(cs, arg...)
			cmd := exec.CommandContext(ctx, os.Args[0], cs...)
			cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1", "GO_HELPER_PROCESS_ERROR=1", "GOCOVERDIR=/tmp"}
			return cmd
		}

		out, err := service.CheckPlatformReqs(t.Context(), "/tmp")
		require.Error(t, err)
		assert.Empty(t, out)
	})

	t.Run("unparsable output without exec error", func(t *testing.T) {
		service.newCommand = func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--", "not json"}
			cs = append(cs, arg...)
			cmd := exec.CommandContext(ctx, os.Args[0], cs...)
			cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1", "GOCOVERDIR=/tmp"}
			return cmd
		}

		out, err := service.CheckPlatformReqs(t.Context(), "/tmp")
		require.Error(t, err)
		assert.Contains(t, out, "not json")
	})
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

func TestGetConfigReadsComposerJSONAsComposerConfigPrintsIt(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/project/composer.json", []byte(`{
		"extra": {
			"drupal-scaffold": {"locations": {"web-root": "web/"}},
			"patches": {"drupal/core": {"Fix": "patches/core.patch"}}
		}
	}`), 0o644))
	service := &CLI{logger: zap.NewNop(), fs: fs}

	webRoot, err := service.GetConfig(t.Context(), "/project", "extra.drupal-scaffold.locations.web-root")
	require.NoError(t, err)
	assert.Equal(t, "web/", webRoot, "a string comes back unquoted")

	patches, err := service.GetConfig(t.Context(), "/project", "extra.patches")
	require.NoError(t, err)
	assert.JSONEq(t, `{"drupal/core": {"Fix": "patches/core.patch"}}`, patches)

	for _, key := range []string{"extra.missing", "extra.drupal-scaffold.locations.web-root.deeper"} {
		_, err := service.GetConfig(t.Context(), "/project", key)
		require.EqualError(t, err, key+" is not defined")
	}
}

func TestGetConfigFailsWithoutAReadableComposerJSON(t *testing.T) {
	fs := afero.NewMemMapFs()
	service := &CLI{logger: zap.NewNop(), fs: fs}

	_, err := service.GetConfig(t.Context(), "/project", "extra.patches")
	require.ErrorIs(t, err, os.ErrNotExist)

	require.NoError(t, afero.WriteFile(fs, "/project/composer.json", []byte("{"), 0o644))
	_, err = service.GetConfig(t.Context(), "/project", "extra.patches")
	require.ErrorContains(t, err, "failed to decode composer.json")
}

func TestSetConfig(t *testing.T) {
	service := &CLI{logger: zap.NewNop()}

	t.Run("success", func(t *testing.T) {
		service.newCommand = func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--", "ok"}
			cs = append(cs, arg...)
			cmd := exec.CommandContext(ctx, os.Args[0], cs...)
			cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1", "GOCOVERDIR=/tmp"}
			return cmd
		}
		require.NoError(t, service.SetConfig(t.Context(), "/tmp", "platform.php", "8.3"))
	})

	t.Run("error", func(t *testing.T) {
		service.newCommand = func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--"}
			cs = append(cs, arg...)
			cmd := exec.CommandContext(ctx, os.Args[0], cs...)
			cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1", "GO_HELPER_PROCESS_ERROR=1", "GOCOVERDIR=/tmp"}
			return cmd
		}
		require.Error(t, service.SetConfig(t.Context(), "/tmp", "platform.php", "8.3"))
	})
}

func TestIsPackageInstalled(t *testing.T) {
	service := &CLI{logger: zap.NewNop()}

	t.Run("installed", func(t *testing.T) {
		service.newCommand = func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--", "ok"}
			cs = append(cs, arg...)
			cmd := exec.CommandContext(ctx, os.Args[0], cs...)
			cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1", "GOCOVERDIR=/tmp"}
			return cmd
		}
		installed, err := service.IsPackageInstalled(t.Context(), "/tmp", "drupal/core")
		require.NoError(t, err)
		assert.True(t, installed)
	})

	t.Run("not installed", func(t *testing.T) {
		service.newCommand = func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--"}
			cs = append(cs, arg...)
			cmd := exec.CommandContext(ctx, os.Args[0], cs...)
			cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1", "GO_HELPER_PROCESS_ERROR=1", "GOCOVERDIR=/tmp"}
			return cmd
		}
		installed, err := service.IsPackageInstalled(t.Context(), "/tmp", "drupal/core")
		require.NoError(t, err)
		assert.False(t, installed)
	})
}

func TestUpdateLockHash(t *testing.T) {
	service := &CLI{logger: zap.NewNop()}

	t.Run("success", func(t *testing.T) {
		service.newCommand = func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--", "ok"}
			cs = append(cs, arg...)
			cmd := exec.CommandContext(ctx, os.Args[0], cs...)
			cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1", "GOCOVERDIR=/tmp"}
			return cmd
		}
		require.NoError(t, service.UpdateLockHash(t.Context(), "/tmp"))
	})

	t.Run("error", func(t *testing.T) {
		service.newCommand = func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--"}
			cs = append(cs, arg...)
			cmd := exec.CommandContext(ctx, os.Args[0], cs...)
			cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1", "GO_HELPER_PROCESS_ERROR=1", "GOCOVERDIR=/tmp"}
			return cmd
		}
		require.Error(t, service.UpdateLockHash(t.Context(), "/tmp"))
	})
}

func TestGetCustomCodeDirectories(t *testing.T) {
	t.Run("returns existing directories", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		_ = fs.MkdirAll("/tmp/web/modules/custom", 0755)
		_ = fs.MkdirAll("/tmp/web/themes/custom", 0755)
		require.NoError(t, afero.WriteFile(fs, "/tmp/composer.json", []byte(`{"extra": {"drupal-scaffold": {"locations": {"web-root": "web/"}}}}`), 0o644))

		service := &CLI{logger: zap.NewNop(), fs: fs}

		dirs, err := service.GetCustomCodeDirectories(t.Context(), "/tmp")
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"web/modules/custom", "web/themes/custom"}, dirs)
	})

	t.Run("GetConfig error", func(t *testing.T) {
		service := &CLI{logger: zap.NewNop(), fs: afero.NewMemMapFs()}

		_, err := service.GetCustomCodeDirectories(t.Context(), "/project")
		require.Error(t, err)
	})
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

func TestGetDependencyPatches(t *testing.T) {
	// A dependency provides a patch; another package has empty extra serialized as [].
	data := `{
		"packages": [
			{
				"name": "drupal/lightning_core",
				"extra": {
					"patches": {
						"drupal/core": {
							"2869592 - Disabled update module": "https://example.com/2869592.patch"
						}
					}
				}
			},
			{ "name": "drupal/empty_extra", "extra": [] },
			{ "name": "drupal/no_extra" }
		],
		"packages-dev": [
			{
				"name": "drupal/devtools",
				"extra": { "patches": { "drupal/views": { "fix": "https://example.com/views.patch" } } }
			}
		]
	}`

	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/test/composer.lock", []byte(data), 0644))

	service := &CLI{logger: zap.NewNop(), fs: fs}
	patches, err := service.GetDependencyPatches(context.Background(), "/test")

	require.NoError(t, err)
	assert.Equal(t, map[string]map[string]bool{
		"drupal/core":  {"https://example.com/2869592.patch": true},
		"drupal/views": {"https://example.com/views.patch": true},
	}, patches)

	t.Run("error when composer.lock is missing", func(t *testing.T) {
		service := &CLI{logger: zap.NewNop(), fs: afero.NewMemMapFs()}
		_, err := service.GetDependencyPatches(context.Background(), "/missing")
		require.ErrorContains(t, err, "failed to read composer.lock")
	})

	t.Run("error when composer.lock is invalid JSON", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		require.NoError(t, afero.WriteFile(fs, "/bad/composer.lock", []byte("not-json"), 0644))
		service := &CLI{logger: zap.NewNop(), fs: fs}
		_, err := service.GetDependencyPatches(context.Background(), "/bad")
		require.ErrorContains(t, err, "failed to unmarshal composer.lock")
	})

	t.Run("returns empty map when no dependency declares patches", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		require.NoError(t, afero.WriteFile(fs, "/none/composer.lock", []byte(`{"packages":[{"name":"a/b"}]}`), 0644))
		service := &CLI{logger: zap.NewNop(), fs: fs}
		patches, err := service.GetDependencyPatches(context.Background(), "/none")
		require.NoError(t, err)
		assert.Empty(t, patches)
	})
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
	newCommand := func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
		args = arg
		cs := append([]string{"-test.run=TestHelperProcess", "--", ""}, arg...)
		cmd := exec.CommandContext(ctx, os.Args[0], cs...)
		cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1", "GOCOVERDIR=/tmp"}
		return cmd
	}

	require.NoError(t, (&CLI{newCommand: newCommand, logger: zap.NewNop()}).UnsetConfig(t.Context(), "/tmp", "allow-plugins.*"))
	assert.Equal(t, []string{"config", "--unset", "allow-plugins.*"}, args)

	require.Error(t, (&CLI{newCommand: composerFailing(), logger: zap.NewNop()}).UnsetConfig(t.Context(), "/tmp", "allow-plugins.*"))
}

func TestInstallReturnsNilOnSuccess(t *testing.T) {
	service := &CLI{newCommand: composerPrinting("Nothing to install"), logger: zap.NewNop()}
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
		service := &CLI{newCommand: composerFailing(), logger: zap.NewNop()}
		_, err := service.GetInstalledPackageVersion(t.Context(), "/tmp", "drupal/core")
		require.Error(t, err)
	})

	t.Run("malformed JSON", func(t *testing.T) {
		service := &CLI{newCommand: composerPrinting("not-json"), logger: zap.NewNop()}
		_, err := service.GetInstalledPackageVersion(t.Context(), "/tmp", "drupal/core")
		require.Error(t, err)
	})

	t.Run("no versions reported", func(t *testing.T) {
		service := &CLI{newCommand: composerPrinting(`{"versions":[]}`), logger: zap.NewNop()}
		_, err := service.GetInstalledPackageVersion(t.Context(), "/tmp", "drupal/core")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no versions found")
	})
}

func TestDiffFailure(t *testing.T) {
	service := &CLI{newCommand: composerFailing(), logger: zap.NewNop()}
	entries, err := service.Diff(t.Context(), "/tmp")
	require.Error(t, err)
	assert.Nil(t, entries)
}

func TestGetInstalledPluginsFailure(t *testing.T) {
	service := &CLI{newCommand: composerFailing(), logger: zap.NewNop()}
	_, err := service.GetInstalledPlugins(t.Context(), "/tmp")
	require.Error(t, err)
}

func TestAuditMalformedOutput(t *testing.T) {
	service := &CLI{newCommand: composerPrinting("not-json"), logger: zap.NewNop()}
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
