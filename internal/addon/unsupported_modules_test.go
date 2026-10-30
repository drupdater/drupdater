package addon

import (
	"context"
	"testing"

	"github.com/drupdater/drupdater/internal/workflow"
	"github.com/drupdater/drupdater/pkg/drush"
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
	assert.Empty(t, um.modules.All())
}

func TestUnsupportedModulesRecordsTheSitesModules(t *testing.T) {
	um := NewUnsupportedModules(zap.NewNop())
	modules := []drush.UnsupportedModule{{Name: "module_a", InstalledVersion: "1.0.0", RecommendedVersion: "None"}}

	require.NoError(t, um.preSiteUpdateHandler(unsupportedEvent("default", drush.Facts{UnsupportedModules: modules})))

	assert.Equal(t, map[string][]drush.UnsupportedModule{"default": modules}, um.modules.All())
}

func TestUnsupportedModulesReportsAModuleTwoSitesShareOnce(t *testing.T) {
	um := NewUnsupportedModules(zap.NewNop())
	shared := drush.UnsupportedModule{Name: "module_a", InstalledVersion: "1.0.0", RecommendedVersion: "None"}

	require.NoError(t, um.preSiteUpdateHandler(unsupportedEvent("site1", drush.Facts{UnsupportedModules: []drush.UnsupportedModule{shared}})))
	require.NoError(t, um.preSiteUpdateHandler(unsupportedEvent("site2", drush.Facts{UnsupportedModules: []drush.UnsupportedModule{shared}})))

	assert.Equal(t, []drush.UnsupportedModule{shared}, um.ReportData())
}

func TestUnsupportedModulesRecordsNothingWithoutModules(t *testing.T) {
	um := NewUnsupportedModules(zap.NewNop())

	require.NoError(t, um.preSiteUpdateHandler(unsupportedEvent("default", drush.Facts{})))

	assert.Empty(t, um.modules.All())
}

// Best-effort: an unreachable update status service must not abort the run.
func TestUnsupportedModulesOnlyWarnsWhenTheStatusServiceFailed(t *testing.T) {
	core, logs := observer.New(zapcore.WarnLevel)
	um := NewUnsupportedModules(zap.New(core))
	facts := drush.Facts{
		UnsupportedModulesError: "updates.drupal.org unreachable",
		UnsupportedModules:      []drush.UnsupportedModule{{Name: "ignored"}},
	}

	require.NoError(t, um.preSiteUpdateHandler(unsupportedEvent("default", facts)))

	assert.Empty(t, um.modules.All())
	require.Equal(t, 1, logs.Len())
	assert.Equal(t, "updates.drupal.org unreachable", logs.All()[0].ContextMap()["error"])
}
