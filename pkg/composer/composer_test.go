package composer

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestExecComposer(t *testing.T) {

	// Create an instance of DefaultComposerService
	service := &CLI{
		logger: zap.NewNop(),
	}

	t.Run("successful execution", func(t *testing.T) {
		execCommand = func(_ context.Context, name string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--", name}
			cs = append(cs, arg...)
			cmd := exec.Command(os.Args[0], cs...)
			cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1", "GOCOVERDIR=/tmp"}
			return cmd
		}
		defer func() { execCommand = exec.CommandContext }()

		output, err := service.execComposer(t.Context(), "/tmp", "update")
		assert.NoError(t, err)
		assert.Equal(t, "composer", output)
	})

	t.Run("execution failure", func(t *testing.T) {
		execCommand = func(_ context.Context, name string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--", name}
			cs = append(cs, arg...)
			cmd := exec.Command(os.Args[0], cs...)
			cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1", "GO_HELPER_PROCESS_ERROR=1", "GOCOVERDIR=/tmp"}
			return cmd
		}
		defer func() { execCommand = exec.CommandContext }()

		output, err := service.execComposer(t.Context(), "/tmp", "update")
		assert.Error(t, err)
		assert.Equal(t, "", output)
	})

}

func TestGetComposerUpdates(t *testing.T) {

	logData := `- Removing behat/mink-selenium2-driver (v1.7.0)
- Removing instaclick/php-webdriver (1.4.19)
- Upgrading behat/mink (v1.11.0 => v1.12.0)
- Downgrading behat/foo (v1.12.0 => v1.11.0)
- Installing tbachert/spi (v1.0.2)`

	fs := afero.NewMemMapFs()

	// Create an instance of DefaultComposerService
	service := &CLI{
		logger: zap.NewNop(),
		fs:     fs,
	}

	execCommand = func(_ context.Context, _ string, arg ...string) *exec.Cmd {
		cs := []string{"-test.run=TestHelperProcess", "--", logData}
		cs = append(cs, arg...)
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1", "GOCOVERDIR=/tmp"}
		return cmd
	}
	defer func() { execCommand = exec.CommandContext }()

	changes, err := service.Update(t.Context(), "/tmp", []string{}, []string{}, false, true)

	assert.NoError(t, err)
	assert.Len(t, changes, 5)

	assert.Equal(t, "Upgrade", changes[0].Action)
	assert.Equal(t, "behat/mink", changes[0].Package)
	assert.Equal(t, "v1.11.0", changes[0].From)
	assert.Equal(t, "v1.12.0", changes[0].To)

	assert.Equal(t, "Downgrade", changes[1].Action)
	assert.Equal(t, "behat/foo", changes[1].Package)
	assert.Equal(t, "v1.12.0", changes[1].From)
	assert.Equal(t, "v1.11.0", changes[1].To)

	assert.Equal(t, "Remove", changes[2].Action)
	assert.Equal(t, "behat/mink-selenium2-driver", changes[2].Package)
	assert.Equal(t, "v1.7.0", changes[2].From)
	assert.Equal(t, "", changes[2].To)

	assert.Equal(t, "Remove", changes[3].Action)
	assert.Equal(t, "instaclick/php-webdriver", changes[3].Package)
	assert.Equal(t, "1.4.19", changes[3].From)
	assert.Equal(t, "", changes[3].To)

	assert.Equal(t, "Install", changes[4].Action)
	assert.Equal(t, "tbachert/spi", changes[4].Package)
	assert.Equal(t, "", changes[4].From)
	assert.Equal(t, "v1.0.2", changes[4].To)
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

		execCommand = func(_ context.Context, _ string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--", data}
			cs = append(cs, arg...)
			cmd := exec.Command(os.Args[0], cs...)
			cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1", "GOCOVERDIR=/tmp"}
			return cmd
		}
		defer func() { execCommand = exec.CommandContext }()

		service := &CLI{
			logger: zap.NewNop(),
		}

		plugins, err := service.GetInstalledPlugins(t.Context(), "/tmp")

		assert.NoError(t, err)
		assert.Equal(t, map[string]interface{}{
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

		execCommand = func(_ context.Context, _ string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--", data}
			cs = append(cs, arg...)
			cmd := exec.Command(os.Args[0], cs...)
			cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1", "GOCOVERDIR=/tmp"}
			return cmd
		}
		defer func() { execCommand = exec.CommandContext }()

		audit, err := service.Audit(t.Context(), "/tmp")

		assert.NoError(t, err)
		assert.Nil(t, audit.Advisories)
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
	   	        "j7mbo/twitter-api-php": null
	   	    }
	   	}`

		execCommand = func(_ context.Context, _ string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--", data}
			cs = append(cs, arg...)
			cmd := exec.Command(os.Args[0], cs...)
			cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1", "GOCOVERDIR=/tmp"}
			return cmd
		}
		defer func() { execCommand = exec.CommandContext }()

		service := &CLI{
			logger: zap.NewNop(),
		}

		audit, err := service.Audit(t.Context(), "/tmp")

		assert.NoError(t, err)

		assert.Len(t, audit.Advisories, 1)
		assert.Equal(t, audit.Advisories, []Advisory{
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
		})
	})
}

func TestGetComposerLockHash(t *testing.T) {
	data := `{
	   		"content-hash": "d3d29b1f6a1d8f2c3b9b8e1e4f5f9e3e"
	   	}`

	fs := afero.NewMemMapFs()
	err := afero.WriteFile(fs, "/test/composer.lock", []byte(data), 0644)
	assert.NoError(t, err)

	service := &CLI{
		logger: zap.NewNop(),
		fs:     fs,
	}
	hash, err := service.GetLockHash("/test")

	assert.NoError(t, err)
	assert.Equal(t, "d3d29b1f6a1d8f2c3b9b8e1e4f5f9e3e", hash)
}

func TestCheckPatchApplies(t *testing.T) {

	t.Run("Patch applies", func(t *testing.T) {

		fs := afero.NewOsFs()

		out := `Running composer update drupal/core
Loading composer repositories with package information
Updating dependencies
Lock file operations: 58 installs, 0 updates, 0 removals
  - Locking twig/twig (v3.20.0)
Writing lock file
Installing dependencies from lock file (including require-dev)
Package operations: 58 installs, 0 updates, 0 removals
  - Installing doctrine/deprecations (1.1.5): Extracting archive
Generating autoload files
Using version ^11.1 for drupal/core`

		execCommand = func(_ context.Context, _ string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--", out}
			cs = append(cs, arg...)
			cmd := exec.Command(os.Args[0], cs...)
			cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1", "GOCOVERDIR=/tmp"}
			return cmd
		}
		defer func() { execCommand = exec.CommandContext }()

		service := &CLI{
			logger: zap.NewNop(),
			fs:     fs,
		}

		applies, err := service.CheckIfPatchApplies(t.Context(), "drupal/core", "1.0.0", "path/to/patch")
		assert.NoError(t, err)
		assert.True(t, applies)
	})

	t.Run("Patch does not apply", func(t *testing.T) {

		fs := afero.NewOsFs()

		service := &CLI{
			logger: zap.NewNop(),
			fs:     fs,
		}

		execCommand = func(_ context.Context, _ string, arg ...string) *exec.Cmd {
			cs := []string{"-test.run=TestHelperProcess", "--"}
			cs = append(cs, arg...)
			cmd := exec.Command(os.Args[0], cs...)
			cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1", "GO_HELPER_PROCESS_ERROR=1", "GOCOVERDIR=/tmp"}
			return cmd
		}
		defer func() { execCommand = exec.CommandContext }()

		applies, err := service.CheckIfPatchApplies(t.Context(), "drupal/core", "1.0.0", "path/to/patch")
		assert.NoError(t, err)
		assert.False(t, applies)
	})

}

func TestHelperProcess(*testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	if os.Getenv("GO_HELPER_PROCESS_ERROR") == "1" {
		os.Exit(1)
	}

	fmt.Fprintf(os.Stdout, "%v\n", os.Args[3])
	os.Exit(0)
}
