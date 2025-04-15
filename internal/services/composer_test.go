package services

import (
	"testing"

	"drupdater/internal/utils"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.uber.org/zap"
)

func TestGetComposerUpdates(t *testing.T) {

	logData := `- Removing behat/mink-selenium2-driver (v1.7.0)
- Removing instaclick/php-webdriver (1.4.19)
- Upgrading behat/mink (v1.11.0 => v1.12.0)
- Downgrading behat/foo (v1.12.0 => v1.11.0)
- Installing tbachert/spi (v1.0.2)`

	commandExecutor := utils.NewMockCommandExecutor(t)
	fs := afero.NewMemMapFs()

	// Create an instance of DefaultComposerService
	service := &DefaultComposerService{
		logger:          zap.NewNop(),
		commandExecutor: commandExecutor,
		fs:              fs,
	}

	commandExecutor.On("UpdateDependencies", "/test", []string{}, false, true).Return(logData, nil)
	changes, err := service.GetComposerUpdates("/test", []string{}, false)

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

	commandExecutor.AssertExpectations(t)

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

		commandExecutor := utils.NewMockCommandExecutor(t)
		commandExecutor.On("ExecComposer", "/test", "depends", "composer-plugin-api", "--locked").Return(data, nil)

		service := &DefaultComposerService{
			logger:          zap.NewNop(),
			commandExecutor: commandExecutor,
		}

		plugins, err := service.GetInstalledPlugins("/test")

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

		commandExecutor := utils.NewMockCommandExecutor(t)
		commandExecutor.On("ExecComposer", "/test", "audit", "--format=json", "--locked", "--no-plugins").Return(data, nil)

		service := &DefaultComposerService{
			logger:          zap.NewNop(),
			commandExecutor: commandExecutor,
		}

		audit, err := service.RunComposerAudit("/test")

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

		commandExecutor := utils.NewMockCommandExecutor(t)
		commandExecutor.On("ExecComposer", "/test", "audit", "--format=json", "--locked", "--no-plugins").Return(data, nil)

		service := &DefaultComposerService{
			logger:          zap.NewNop(),
			commandExecutor: commandExecutor,
		}

		audit, err := service.RunComposerAudit("/test")

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

	service := &DefaultComposerService{
		logger: zap.NewNop(),
		fs:     fs,
	}
	hash, err := service.GetComposerLockHash("/test")

	assert.NoError(t, err)
	assert.Equal(t, "d3d29b1f6a1d8f2c3b9b8e1e4f5f9e3e", hash)
}

func TestCheckPatchApplies(t *testing.T) {

	t.Run("Patch applies", func(t *testing.T) {

		fs := afero.NewMemMapFs()
		commandExecutor := utils.NewMockCommandExecutor(t)
		commandExecutor.On("InstallPackages", mock.Anything, "drupal/core:1.0.0", "--with-all-dependencies").Return("", nil)

		service := &DefaultComposerService{
			logger:          zap.NewNop(),
			fs:              fs,
			commandExecutor: commandExecutor,
		}

		applies, err := service.CheckPatchApplies("drupal/core", "1.0.0", "path/to/patch")
		assert.NoError(t, err)
		assert.True(t, applies)
	})

	t.Run("Patch does not apply", func(t *testing.T) {

		fs := afero.NewMemMapFs()
		commandExecutor := utils.NewMockCommandExecutor(t)
		commandExecutor.On("InstallPackages", mock.Anything, "drupal/core:1.0.0", "--with-all-dependencies").Return("", assert.AnError)

		service := &DefaultComposerService{
			logger:          zap.NewNop(),
			fs:              fs,
			commandExecutor: commandExecutor,
		}

		applies, err := service.CheckPatchApplies("drupal/core", "1.0.0", "path/to/patch")
		assert.NoError(t, err)
		assert.False(t, applies)
	})
}
