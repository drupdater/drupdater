package addon

import (
	"context"
	"testing"

	"github.com/drupdater/drupdater/internal/drush"
	"github.com/drupdater/drupdater/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func unsupportedEvent(site string, facts drush.Facts) *workflow.PreSiteUpdateEvent {
	return workflow.NewPreSiteUpdateEvent(context.Background(), "/test/path", &fakeCommitter{}, site, facts)
}

func TestNewUnsupportedModules(t *testing.T) {
	logger := zap.NewNop()

	um := NewUnsupportedModules(logger)

	assert.NotNil(t, um)
	assert.Equal(t, logger, um.logger)
}

func TestUnsupportedModulesFindsTheSitesModules(t *testing.T) {
	um := NewUnsupportedModules(zap.NewNop())
	modules := []drush.UnsupportedModule{{Name: "module_a", InstalledVersion: "1.0.0", RecommendedVersion: "None"}}

	found, err := um.preSiteUpdateHandler(unsupportedEvent("default", drush.Facts{UnsupportedModules: modules}))

	require.NoError(t, err)
	assert.Equal(t, modules, found)
}

func TestUnsupportedModulesReportsAModuleTwoSitesShareOnce(t *testing.T) {
	um := NewUnsupportedModules(zap.NewNop())
	p := workflow.NewPipeline([]workflow.Addon{um})
	shared := drush.UnsupportedModule{Name: "module_a", InstalledVersion: "1.0.0", RecommendedVersion: "None"}

	require.NoError(t, workflow.Fire(p, unsupportedEvent("site1", drush.Facts{UnsupportedModules: []drush.UnsupportedModule{shared}})))
	require.NoError(t, workflow.Fire(p, unsupportedEvent("site2", drush.Facts{UnsupportedModules: []drush.UnsupportedModule{shared}})))

	assert.Equal(t, []drush.UnsupportedModule{shared}, reportSection(p, "unsupported_modules"))
}

func TestUnsupportedModulesLogsOnlyASiteWithModules(t *testing.T) {
	core, logs := observer.New(zapcore.InfoLevel)
	um := NewUnsupportedModules(zap.New(core))

	found, err := um.preSiteUpdateHandler(unsupportedEvent("default", drush.Facts{}))
	require.NoError(t, err)
	assert.Empty(t, found)
	assert.Zero(t, logs.Len())

	_, err = um.preSiteUpdateHandler(unsupportedEvent("second", drush.Facts{UnsupportedModules: []drush.UnsupportedModule{{Name: "module_a"}}}))
	require.NoError(t, err)
	require.Equal(t, 1, logs.Len())
	assert.Equal(t, map[string]any{"site": "second", "count": int64(1)}, logs.All()[0].ContextMap())
}

// Best-effort: an unreachable update status service must not abort the run.
func TestUnsupportedModulesOnlyWarnsWhenTheStatusServiceFailed(t *testing.T) {
	core, logs := observer.New(zapcore.WarnLevel)
	um := NewUnsupportedModules(zap.New(core))
	facts := drush.Facts{
		UnsupportedModulesError: "updates.drupal.org unreachable",
		UnsupportedModules:      []drush.UnsupportedModule{{Name: "ignored"}},
	}

	found, err := um.preSiteUpdateHandler(unsupportedEvent("default", facts))

	require.NoError(t, err)
	assert.Nil(t, found)
	require.Equal(t, 1, logs.Len())
	assert.Equal(t, "updates.drupal.org unreachable", logs.All()[0].ContextMap()["error"])
}
