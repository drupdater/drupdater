package composer

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// stubComposerFailureWithOutput makes the next composer invocations print out and exit non-zero,
// which is what composer does when it cannot resolve a package or cannot apply a patch.
func stubComposerFailureWithOutput(t *testing.T, out string) {
	t.Helper()
	execCommand = func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
		cs := append([]string{"-test.run=TestHelperProcess", "--"}, arg...)
		cmd := exec.CommandContext(ctx, os.Args[0], cs...)
		cmd.Env = []string{
			"GO_WANT_HELPER_PROCESS=1",
			"GO_HELPER_PROCESS_ERROR=1",
			"GO_HELPER_PROCESS_OUTPUT=" + out,
			"GOCOVERDIR=/tmp",
		}
		return cmd
	}
	t.Cleanup(func() { execCommand = exec.CommandContext })
}

// newScratchCLI returns a CLI backed by the real filesystem, plus a project directory holding
// projectComposerJSON (or no composer.json at all, when it is empty).
//
// The real filesystem, not a MemMapFs: the scratch project's temp directory becomes a
// subprocess's working directory, and a directory that exists only in memory cannot be one.
func newScratchCLI(t *testing.T, projectComposerJSON string) (*CLI, string) {
	t.Helper()
	projectDir := t.TempDir()
	if projectComposerJSON != "" {
		require.NoError(t, os.WriteFile(filepath.Join(projectDir, "composer.json"), []byte(projectComposerJSON), 0600))
	}
	service := &CLI{logger: zap.NewNop(), fs: afero.NewOsFs()}
	t.Cleanup(service.Cleanup)
	return service, projectDir
}

func TestBuildScratchComposerJSON(t *testing.T) {
	t.Run("carries the project's repositories, in order, ahead of the drupal.org fallback", func(t *testing.T) {
		// The regression this guards: a package served only from a private registry could not be
		// resolved in the scratch project at all, so its patch check failed for a reason that had
		// nothing to do with the patch.
		service, projectDir := newScratchCLI(t, `{
			"repositories": [
				{"type": "composer", "url": "https://repo.packagist.com/acme/"},
				{"type": "vcs", "url": "https://github.com/acme/private-module"}
			]
		}`)

		out, err := service.buildScratchComposerJSON(projectDir)
		require.NoError(t, err)

		var project struct {
			Repositories []map[string]any `json:"repositories"`
		}
		require.NoError(t, json.Unmarshal(out, &project))
		require.Len(t, project.Repositories, 3)
		assert.Equal(t, "https://repo.packagist.com/acme/", project.Repositories[0]["url"])
		assert.Equal(t, "https://github.com/acme/private-module", project.Repositories[1]["url"])
		assert.Equal(t, drupalOrgRepositoryURL, project.Repositories[2]["url"],
			"the drupal.org fallback must come last so the project's own repositories keep priority")
	})

	t.Run("accepts the object form and sorts it for a byte-stable result", func(t *testing.T) {
		service, projectDir := newScratchCLI(t, `{
			"repositories": {
				"zeta": {"type": "composer", "url": "https://zeta.example.com"},
				"alpha": {"type": "composer", "url": "https://alpha.example.com"}
			}
		}`)

		first, err := service.buildScratchComposerJSON(projectDir)
		require.NoError(t, err)
		second, err := service.buildScratchComposerJSON(projectDir)
		require.NoError(t, err)
		assert.Equal(t, string(first), string(second))

		var project struct {
			Repositories []map[string]any `json:"repositories"`
		}
		require.NoError(t, json.Unmarshal(first, &project))
		assert.Equal(t, "https://alpha.example.com", project.Repositories[0]["url"])
		assert.Equal(t, "https://zeta.example.com", project.Repositories[1]["url"])
	})

	t.Run("drops a packagist.org disable entry", func(t *testing.T) {
		// The scratch project needs packagist.org to resolve cweagans/composer-patches, even for
		// a project that routes everything through a mirror.
		service, projectDir := newScratchCLI(t, `{
			"repositories": [
				{"type": "composer", "url": "https://repo.packagist.com/acme/"},
				{"packagist.org": false}
			]
		}`)

		out, err := service.buildScratchComposerJSON(projectDir)
		require.NoError(t, err)
		assert.NotContains(t, string(out), "packagist.org")
	})

	t.Run("keeps a repository that merely carries a boolean option", func(t *testing.T) {
		// canonical:false is how a mirror is declared -- it is a real repository, not a disable.
		service, projectDir := newScratchCLI(t, `{
			"repositories": [
				{"type": "composer", "url": "https://mirror.example.com", "canonical": false}
			]
		}`)

		out, err := service.buildScratchComposerJSON(projectDir)
		require.NoError(t, err)
		assert.Contains(t, string(out), "https://mirror.example.com")
	})

	t.Run("resolves a relative path repository against the project", func(t *testing.T) {
		service, projectDir := newScratchCLI(t, `{
			"repositories": [
				{"type": "path", "url": "./modules/custom/*"},
				{"type": "path", "url": "/opt/shared/module"}
			]
		}`)

		out, err := service.buildScratchComposerJSON(projectDir)
		require.NoError(t, err)
		assert.Contains(t, string(out), filepath.Join(projectDir, "modules/custom/*"),
			"a relative path points nowhere from the scratch project's temp directory")
		assert.Contains(t, string(out), "/opt/shared/module")
	})

	t.Run("does not duplicate a drupal.org repository the project already declares", func(t *testing.T) {
		service, projectDir := newScratchCLI(t, `{
			"repositories": [{"type": "composer", "url": "`+drupalOrgRepositoryURL+`"}]
		}`)

		out, err := service.buildScratchComposerJSON(projectDir)
		require.NoError(t, err)

		var project struct {
			Repositories []map[string]any `json:"repositories"`
		}
		require.NoError(t, json.Unmarshal(out, &project))
		assert.Len(t, project.Repositories, 1)
	})

	t.Run("falls back to drupal.org alone when the project's composer.json cannot be used", func(t *testing.T) {
		for name, projectJSON := range map[string]string{
			"missing":       "",
			"unparseable":   `{"repositories": [`,
			"no key":        `{"require": {}}`,
			"wrong shape":   `{"repositories": 42}`,
			"empty entries": `{"repositories": []}`,
		} {
			t.Run(name, func(t *testing.T) {
				service, projectDir := newScratchCLI(t, projectJSON)

				out, err := service.buildScratchComposerJSON(projectDir)
				require.NoError(t, err)

				var project struct {
					Repositories []map[string]any `json:"repositories"`
					Require      map[string]string
				}
				require.NoError(t, json.Unmarshal(out, &project))
				require.Len(t, project.Repositories, 1)
				assert.Equal(t, drupalOrgRepositoryURL, project.Repositories[0]["url"])
				assert.Contains(t, project.Require, "cweagans/composer-patches")
			})
		}
	})

	t.Run("skips an unreadable working directory", func(t *testing.T) {
		service, _ := newScratchCLI(t, "")
		out, err := service.buildScratchComposerJSON("")
		require.NoError(t, err)
		assert.Contains(t, string(out), drupalOrgRepositoryURL)
	})
}

func TestUnresolvableReason(t *testing.T) {
	for name, tc := range map[string]struct {
		out          string
		unresolvable bool
	}{
		"package missing":      {"Could not find package acme/private in any version", true},
		"no matching version":  {"Could not find a matching version of package drupal/foo", true},
		"not found phrasing":   {"Root composer.json requires acme/private, it could not be found", true},
		"bad credentials":      {"Invalid credentials for 'https://repo.packagist.com/acme/'", true},
		"auth required":        {"Authentication required (repo.packagist.com):", true},
		"download failed":      {"The 'https://repo.packagist.com/acme/p2/x.json' file could not be downloaded", true},
		"patch was rejected":   {"  - Applying patches for drupal/core\nCould not apply patch! Skipping.", false},
		"unrelated failure":    {"Your requirements could not be resolved to an installable set of packages.", false},
		"version conflict":     {"drupal/core 10.1.0 conflicts with drupal/foo 2.0.0", false},
		"empty output at fail": {"", false},
	} {
		t.Run(name, func(t *testing.T) {
			_, unresolvable := unresolvableReason(tc.out)
			assert.Equal(t, tc.unresolvable, unresolvable)
		})
	}
}

func TestCheckIfPatchAppliesClassifiesFailures(t *testing.T) {
	t.Run("a package composer cannot obtain is an error, not a patch conflict", func(t *testing.T) {
		// Reported as "the patch does not apply", this pinned the package at its current version
		// and told the reviewer a patch conflicted -- on every run, since nothing about it ever
		// changes. The callers leave the package alone on an error instead.
		stubComposerFailureWithOutput(t, "Could not find package acme/private-module in any version")
		service, projectDir := newScratchCLI(t, `{"repositories": []}`)

		applies, err := service.CheckIfPatchApplies(t.Context(), projectDir, "acme/private-module", "1.2.0", filepath.Join(projectDir, "patches/a.diff"))

		require.Error(t, err)
		assert.False(t, applies)
		assert.Contains(t, err.Error(), "could not obtain acme/private-module 1.2.0")
		assert.Contains(t, err.Error(), "not available from any configured repository")
	})

	t.Run("a rejected patch is still a plain false", func(t *testing.T) {
		stubComposerFailureWithOutput(t, "  - Applying patches for drupal/core\nCould not apply patch! Skipping.")
		service, projectDir := newScratchCLI(t, `{"repositories": []}`)

		applies, err := service.CheckIfPatchApplies(t.Context(), projectDir, "drupal/core", "10.2.0", filepath.Join(projectDir, "patches/a.diff"))

		require.NoError(t, err)
		assert.False(t, applies)
	})

	t.Run("the project's repositories reach the scratch project", func(t *testing.T) {
		stubComposerOutput(t, "ok")
		service, projectDir := newScratchCLI(t, `{"repositories": [{"type": "composer", "url": "https://repo.packagist.com/acme/"}]}`)

		applies, err := service.CheckIfPatchApplies(t.Context(), projectDir, "acme/private-module", "1.2.0", filepath.Join(projectDir, "patches/a.diff"))
		require.NoError(t, err)
		assert.True(t, applies)

		content, err := afero.ReadFile(service.fs, service.tempDir+"/composer.json")
		require.NoError(t, err)
		assert.Contains(t, string(content), "https://repo.packagist.com/acme/")
	})
}

func TestCheckIfPatchesApplyClassifiesFailures(t *testing.T) {
	t.Run("a package composer cannot obtain is an error", func(t *testing.T) {
		stubComposerFailureWithOutput(t, "Could not find package acme/private-module in any version")
		service, projectDir := newScratchCLI(t, `{"repositories": []}`)
		patches := []string{filepath.Join(projectDir, "patches/a.diff"), filepath.Join(projectDir, "patches/b.diff")}

		applies, err := service.CheckIfPatchesApply(t.Context(), projectDir, "acme/private-module", "1.2.0", patches)

		require.Error(t, err)
		assert.False(t, applies)
	})

	t.Run("patches that do not apply together are a plain false", func(t *testing.T) {
		stubComposerFailureWithOutput(t, "Could not apply patch! Skipping.")
		service, projectDir := newScratchCLI(t, `{"repositories": []}`)
		patches := []string{filepath.Join(projectDir, "patches/a.diff"), filepath.Join(projectDir, "patches/b.diff")}

		applies, err := service.CheckIfPatchesApply(t.Context(), projectDir, "drupal/core", "10.2.0", patches)

		require.NoError(t, err)
		assert.False(t, applies)
	})
}
