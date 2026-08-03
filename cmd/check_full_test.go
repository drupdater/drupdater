package cmd

import (
	"context"
	"errors"
	"testing"

	"github.com/drupdater/drupdater/internal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// fakeComposer records what the --full tier asked of composer.
type fakeComposer struct {
	installErr    error
	installCalls  []string
	cleanupCalled int
}

func (f *fakeComposer) Install(_ context.Context, path string) error {
	f.installCalls = append(f.installCalls, path)
	return f.installErr
}

func (f *fakeComposer) Cleanup() { f.cleanupCalled++ }

func (f *fakeComposer) GetConfig(_ context.Context, _ string, _ string) (string, error) {
	return "web", nil
}

// fakeInstaller fails for the sites named in failFor and succeeds for the rest.
type fakeInstaller struct {
	failFor map[string]error
	calls   []string
}

func (f *fakeInstaller) Install(_ context.Context, dir string, site string) error {
	f.calls = append(f.calls, dir+"/"+site)
	return f.failFor[site]
}

// stubFullCheckDeps swaps in doubles for the real clone, composer install and site install,
// without which the tier's control flow is untestable.
func stubFullCheckDeps(t *testing.T, clonePath string, cloneErr error, composerDouble *fakeComposer, installer siteInstaller, installerErr error) *[]string {
	t.Helper()

	cloneArgs := &[]string{}
	original := newFullCheckDeps
	newFullCheckDeps = func(*zap.Logger) fullCheckDeps {
		return fullCheckDeps{
			clone: func(repositoryURL string, branch string, token string) (string, error) {
				*cloneArgs = []string{repositoryURL, branch, token}
				return clonePath, cloneErr
			},
			newComposer: func() fullCheckComposer { return composerDouble },
			newInstaller: func(fullCheckComposer) (siteInstaller, error) {
				return installer, installerErr
			},
		}
	}
	t.Cleanup(func() { newFullCheckDeps = original })

	return cloneArgs
}

func TestRunFullChecksClonesTheConfiguredBranch(t *testing.T) {
	comp := &fakeComposer{}
	inst := &fakeInstaller{}
	cloneArgs := stubFullCheckDeps(t, t.TempDir(), nil, comp, inst, nil)

	cfg := internal.Config{RepositoryURL: "https://example.com/acme/site.git", Branch: "develop", Sites: []string{"default"}}
	results := runFullChecks(t.Context(), zap.NewNop(), cfg, "tok")

	assert.Equal(t, []string{"https://example.com/acme/site.git", "develop", "tok"}, *cloneArgs)
	require.Len(t, results, 2)
	assert.True(t, results[0].OK)
	assert.Equal(t, "composer install", results[0].Name)
	assert.True(t, results[1].OK)
	assert.Contains(t, results[1].Name, `site "default" installs from configuration`)

	// composer must be cleaned up even on the happy path -- it leaves a scratch directory
	// behind otherwise.
	assert.Equal(t, 1, comp.cleanupCalled)
}

func TestRunFullChecksDefaultsToMain(t *testing.T) {
	cloneArgs := stubFullCheckDeps(t, t.TempDir(), nil, &fakeComposer{}, &fakeInstaller{}, nil)

	cfg := internal.Config{RepositoryURL: "https://example.com/acme/site.git"}
	runFullChecks(t.Context(), zap.NewNop(), cfg, "")

	require.Len(t, *cloneArgs, 3)
	assert.Equal(t, "main", (*cloneArgs)[1], "an unset branch must fall back to main, not clone HEAD")
}

func TestRunFullChecksStopsAtComposerInstall(t *testing.T) {
	comp := &fakeComposer{installErr: errors.New("lock file out of date")}
	inst := &fakeInstaller{}
	stubFullCheckDeps(t, t.TempDir(), nil, comp, inst, nil)

	cfg := internal.Config{RepositoryURL: "https://example.com/acme/site.git", Sites: []string{"default"}}
	results := runFullChecks(t.Context(), zap.NewNop(), cfg, "")

	// Installing a site on top of a failed composer install would fail for a reason that has
	// nothing to do with the site's configuration, so the tier stops here.
	require.Len(t, results, 1)
	assert.Equal(t, "composer install", results[0].Name)
	assert.False(t, results[0].OK)
	assert.Contains(t, results[0].Detail, "lock file out of date")
	assert.Empty(t, inst.calls, "no site may be installed after composer install failed")
	assert.Equal(t, 1, comp.cleanupCalled)
}

func TestRunFullChecksReportsInstallerConstructionFailure(t *testing.T) {
	comp := &fakeComposer{}
	stubFullCheckDeps(t, t.TempDir(), nil, comp, nil, errors.New("cache unavailable"))

	cfg := internal.Config{RepositoryURL: "https://example.com/acme/site.git", Sites: []string{"default"}}
	results := runFullChecks(t.Context(), zap.NewNop(), cfg, "")

	// The composer install already passed, so its result is kept alongside the failure.
	require.Len(t, results, 2)
	assert.True(t, results[0].OK)
	assert.Equal(t, "drush site-install --existing-config", results[1].Name)
	assert.False(t, results[1].OK)
	assert.Contains(t, results[1].Detail, "cache unavailable")
}

func TestRunFullChecksCollectsPerSiteFailures(t *testing.T) {
	comp := &fakeComposer{}
	inst := &fakeInstaller{failFor: map[string]error{"broken": errors.New("missing config")}}
	stubFullCheckDeps(t, "/clone", nil, comp, inst, nil)

	cfg := internal.Config{
		RepositoryURL: "https://example.com/acme/site.git",
		Sites:         []string{"first", "broken", "last"},
	}
	results := runFullChecks(t.Context(), zap.NewNop(), cfg, "")

	// One site failing must not hide the others: the point of the tier is to report every
	// site's verdict in a single run.
	require.Len(t, results, 4)
	assert.True(t, results[1].OK)
	assert.False(t, results[2].OK)
	assert.Contains(t, results[2].Detail, "missing config")
	assert.True(t, results[3].OK)
	assert.Equal(t, []string{"/clone/first", "/clone/broken", "/clone/last"}, inst.calls)
}

func TestRunFullChecksStopsAtCloneFailure(t *testing.T) {
	comp := &fakeComposer{}
	stubFullCheckDeps(t, "", errors.New("authentication failed"), comp, &fakeInstaller{}, nil)

	cfg := internal.Config{RepositoryURL: "https://example.com/acme/site.git", Sites: []string{"default"}}
	results := runFullChecks(t.Context(), zap.NewNop(), cfg, "")

	require.Len(t, results, 1)
	assert.Equal(t, "clone for full check", results[0].Name)
	assert.False(t, results[0].OK)
	assert.Contains(t, results[0].Detail, "authentication failed")
	// Nothing was constructed past the clone, so there is nothing to clean up either.
	assert.Zero(t, comp.cleanupCalled)
	assert.Empty(t, comp.installCalls)
}
