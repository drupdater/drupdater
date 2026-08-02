package addon

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/drupdater/drupdater/internal/services"
	"github.com/drupdater/drupdater/pkg/composer"
	"github.com/drupdater/drupdater/pkg/drush"
	"github.com/gookit/event"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestNewUnsupportedModules(t *testing.T) {
	logger := zap.NewNop()
	mockDrush := NewMockDrush(t)

	um := NewUnsupportedModules(logger, mockDrush)

	assert.NotNil(t, um)
	assert.Equal(t, logger, um.logger)
	assert.Equal(t, mockDrush, um.drush)
	assert.NotNil(t, um.modules)
	assert.Empty(t, um.modules)
}

func TestUnsupportedModules_SubscribedEvents(t *testing.T) {
	um := &UnsupportedModules{}

	events := um.SubscribedEvents()

	assert.Contains(t, events, "pre-site-update")
	assert.IsType(t, event.ListenerItem{}, events["pre-site-update"])

	// Below Normal on pre-merge-request-create, strictly below the Normal that composer_audit
	// publishes the abandoned packages at. Equal priorities would leave the order to the
	// dispatcher and the list would sometimes be read before it is written.
	assert.Contains(t, events, "pre-merge-request-create")
	preMerge := events["pre-merge-request-create"].(event.ListenerItem)
	assert.Equal(t, event.BelowNormal, preMerge.Priority)
	assert.Less(t, preMerge.Priority, event.Normal)
}

func TestUnsupportedModules_RenderTemplate(t *testing.T) {
	fixture, err := os.ReadFile("testdata/unsupported_modules.md")
	require.NoError(t, err)
	expected := string(fixture)

	logger := zap.NewNop()
	mockDrush := NewMockDrush(t)

	um := NewUnsupportedModules(logger, mockDrush)
	um.modules = map[string]drush.UnsupportedModule{
		"module_b": {Name: "module_b", InstalledVersion: "2.3.1", RecommendedVersion: "3.0.0"},
		"module_a": {Name: "module_a", InstalledVersion: "1.0.0", RecommendedVersion: "None"},
	}
	um.abandoned = []services.AbandonedPackage{
		{Name: "swiftmailer/swiftmailer", Replacement: "symfony/mailer"},
		{Name: "patchwork/jsqueeze"},
	}

	result, err := um.RenderTemplate()

	require.NoError(t, err)
	assert.Equal(t, expected, result)
}

func TestUnsupportedModules_RenderTemplate_Empty(t *testing.T) {
	logger := zap.NewNop()
	mockDrush := NewMockDrush(t)
	um := NewUnsupportedModules(logger, mockDrush)

	result, err := um.RenderTemplate()

	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestUnsupportedModules_PreSiteUpdateHandler_Success(t *testing.T) {
	logger := zap.NewNop()
	mockDrush := NewMockDrush(t)
	um := NewUnsupportedModules(logger, mockDrush)

	ctx := context.Background()
	testPath := "/test/path"
	testSite := "default"
	mockModules := []drush.UnsupportedModule{
		{Name: "module_a", InstalledVersion: "1.0.0", RecommendedVersion: "None"},
	}

	worktree := NewMockWorktree(t)
	mockEvent := services.NewPreSiteUpdateEvent(ctx, testPath, worktree, testSite)

	mockDrush.EXPECT().GetUnsupportedModules(ctx, testPath, testSite).Return(mockModules, nil)

	err := um.preSiteUpdateHandler(mockEvent)

	require.NoError(t, err)
	assert.Equal(t, mockModules[0], um.modules["module_a"])
}

func TestUnsupportedModules_PreSiteUpdateHandler_Dedupe(t *testing.T) {
	logger := zap.NewNop()
	mockDrush := NewMockDrush(t)
	um := NewUnsupportedModules(logger, mockDrush)

	ctx := context.Background()
	worktree := NewMockWorktree(t)

	shared := drush.UnsupportedModule{Name: "module_a", InstalledVersion: "1.0.0", RecommendedVersion: "None"}

	mockDrush.EXPECT().GetUnsupportedModules(ctx, "/test/path", "site1").Return([]drush.UnsupportedModule{shared}, nil)
	mockDrush.EXPECT().GetUnsupportedModules(ctx, "/test/path", "site2").Return([]drush.UnsupportedModule{shared}, nil)

	require.NoError(t, um.preSiteUpdateHandler(services.NewPreSiteUpdateEvent(ctx, "/test/path", worktree, "site1")))
	require.NoError(t, um.preSiteUpdateHandler(services.NewPreSiteUpdateEvent(ctx, "/test/path", worktree, "site2")))

	assert.Len(t, um.modules, 1)
	assert.Equal(t, shared, um.modules["module_a"])
}

func TestUnsupportedModules_PreSiteUpdateHandler_NoModules(t *testing.T) {
	logger := zap.NewNop()
	mockDrush := NewMockDrush(t)
	um := NewUnsupportedModules(logger, mockDrush)

	ctx := context.Background()
	testPath := "/test/path"
	testSite := "default"

	worktree := NewMockWorktree(t)
	mockEvent := services.NewPreSiteUpdateEvent(ctx, testPath, worktree, testSite)

	mockDrush.EXPECT().GetUnsupportedModules(ctx, testPath, testSite).Return(nil, nil)

	err := um.preSiteUpdateHandler(mockEvent)

	require.NoError(t, err)
	assert.Empty(t, um.modules)
}

func TestUnsupportedModules_PreSiteUpdateHandler_Error(t *testing.T) {
	logger := zap.NewNop()
	mockDrush := NewMockDrush(t)
	um := NewUnsupportedModules(logger, mockDrush)

	ctx := context.Background()
	testPath := "/test/path"
	testSite := "default"
	expectedError := errors.New("drush error")

	worktree := NewMockWorktree(t)
	mockEvent := services.NewPreSiteUpdateEvent(ctx, testPath, worktree, testSite)

	mockDrush.EXPECT().GetUnsupportedModules(ctx, testPath, testSite).Return(nil, expectedError)

	// Execute - errors are swallowed (logged, not returned): this is a best-effort,
	// informational check that must not abort the run.
	err := um.preSiteUpdateHandler(mockEvent)

	require.NoError(t, err)
	assert.Empty(t, um.modules)
}

// TestUnsupportedModules_PreMergeRequestCreateHandler checks the handoff from composer_audit:
// the abandoned packages arrive on the event and are rendered in the same list as the modules.
func TestUnsupportedModules_PreMergeRequestCreateHandler(t *testing.T) {
	um := NewUnsupportedModules(zap.NewNop(), NewMockDrush(t))

	evt := &services.PreMergeRequestCreateEvent{
		AbandonedPackages: []services.AbandonedPackage{{Name: "patchwork/jsqueeze"}},
	}
	evt.SetName("pre-merge-request-create")

	require.NoError(t, um.preMergeRequestCreateHandler(evt))
	assert.Equal(t, evt.AbandonedPackages, um.abandoned)
}

// TestUnsupportedModules_RenderTemplate_AbandonedOnly checks a project with no unsupported
// module but an abandoned package still gets the section. Before the two were merged this was
// the case that fell through the gap: nothing on Drupal.org's side to report, and no other
// section to put a Packagist finding in.
func TestUnsupportedModules_RenderTemplate_AbandonedOnly(t *testing.T) {
	um := NewUnsupportedModules(zap.NewNop(), NewMockDrush(t))
	um.abandoned = []services.AbandonedPackage{{Name: "patchwork/jsqueeze"}}

	result, err := um.RenderTemplate()
	require.NoError(t, err)
	assert.Contains(t, result, "| patchwork/jsqueeze | Abandoned package | — | No replacement suggested |")
}

// TestUnsupportedModules_EndOfLifeEntries_Ordering pins the row order: one list sorted by
// name, with the two sources interleaved rather than grouped. Both halves originate from maps,
// so without the sort two runs over an unchanged project would produce different descriptions.
func TestUnsupportedModules_EndOfLifeEntries_Ordering(t *testing.T) {
	um := NewUnsupportedModules(zap.NewNop(), NewMockDrush(t))
	um.modules = map[string]drush.UnsupportedModule{
		"zebra": {Name: "zebra", InstalledVersion: "1.0.0", RecommendedVersion: "2.0.0"},
		"alpha": {Name: "alpha", InstalledVersion: "1.0.0", RecommendedVersion: "None"},
	}
	um.abandoned = []services.AbandonedPackage{
		{Name: "zeta/pkg"},
		{Name: "acme/pkg", Replacement: "acme/next"},
	}

	names := make([]string, 0, 4)
	for _, entry := range um.endOfLifeEntries() {
		names = append(names, entry.Name)
	}
	assert.Equal(t, []string{"acme/pkg", "alpha", "zebra", "zeta/pkg"}, names)
}

// TestUnsupportedModules_EndOfLifeEntries_Recommendations covers the four ways a row's
// recommendation is phrased, including drush's literal "None" for a module with no supported
// release at all — which is a status, not a version to update to.
func TestUnsupportedModules_EndOfLifeEntries_Recommendations(t *testing.T) {
	um := NewUnsupportedModules(zap.NewNop(), NewMockDrush(t))
	um.modules = map[string]drush.UnsupportedModule{
		"none":    {Name: "none", RecommendedVersion: "None"},
		"empty":   {Name: "empty", RecommendedVersion: ""},
		"upgrade": {Name: "upgrade", RecommendedVersion: "3.0.0"},
	}
	um.abandoned = []services.AbandonedPackage{
		{Name: "a/replaced", Replacement: "a/next"},
		{Name: "b/orphan"},
	}

	got := map[string]string{}
	for _, entry := range um.endOfLifeEntries() {
		got[entry.Name] = entry.Recommendation
	}
	assert.Equal(t, map[string]string{
		"none":       "No supported release — replace it",
		"empty":      "No supported release — replace it",
		"upgrade":    "Update to 3.0.0",
		"a/replaced": "Replace with a/next",
		"b/orphan":   "No replacement suggested",
	}, got)
}

// TestUnsupportedModules_AbandonedHandoff runs the whole handoff through a real dispatcher:
// composer_audit publishes on pre-merge-request-create and unsupported_modules renders. It is
// the only test that covers the wiring rather than the two halves — the subscription itself,
// and the priority that puts the write before the read.
func TestUnsupportedModules_AbandonedHandoff(t *testing.T) {
	audit := NewComposerAudit(zap.NewNop(), NewMockComposer(t), false)
	audit.afterAudit = composer.Audit{
		Abandoned: []composer.AbandonedPackage{{PackageName: "swiftmailer/swiftmailer", Replacement: "symfony/mailer"}},
	}
	um := NewUnsupportedModules(zap.NewNop(), NewMockDrush(t))

	manager := event.NewManager("test")
	manager.AddSubscriber(audit)
	manager.AddSubscriber(um)

	err := manager.FireEvent(services.NewPreMergeRequestCreateEvent("July 2026: Drupal Maintenance Updates"))
	require.NoError(t, err)

	result, err := um.RenderTemplate()
	require.NoError(t, err)
	assert.Contains(t, result, "| swiftmailer/swiftmailer | Abandoned package | — | Replace with symfony/mailer |")
}
