package addon

import (
	"maps"
	"slices"
	"strings"
	"sync"

	"github.com/drupdater/drupdater/internal"
	"github.com/drupdater/drupdater/internal/services"
	"github.com/drupdater/drupdater/pkg/drush"
	"github.com/gookit/event"
	"go.uber.org/zap"
)

// UnsupportedModules detects modules Drupal's update status service reports as NOT_SUPPORTED, and
// renders them together with the abandoned packages composer_audit hands it: one finding to a
// reviewer, so one list.
type UnsupportedModules struct {
	internal.BasicAddon
	logger *zap.Logger
	drush  Drush

	// mu guards modules: preSiteUpdateHandler runs concurrently for each site. Keyed by name,
	// so multisite results are deduplicated.
	mu      sync.Mutex
	modules map[string]drush.UnsupportedModule

	// Written once from pre-merge-request-create, after every site's goroutine — no lock needed.
	abandoned []services.AbandonedPackage
}

// EndOfLifeEntry is one row of the merged "unsupported or abandoned" table.
type EndOfLifeEntry struct {
	Name string
	// Status is "Unsupported module" or "Abandoned package".
	Status string
	// InstalledVersion is empty for an abandoned package: `composer audit` reports none.
	InstalledVersion string
	// Recommendation is what to do about it, already phrased for the reader.
	Recommendation string
}

// NewUnsupportedModules creates an unsupported modules detector.
func NewUnsupportedModules(logger *zap.Logger, drushClient Drush) *UnsupportedModules {
	return &UnsupportedModules{
		logger:  logger,
		drush:   drushClient,
		modules: make(map[string]drush.UnsupportedModule),
	}
}

func (um *UnsupportedModules) SubscribedEvents() map[string]any {
	return map[string]any{
		"pre-site-update": event.ListenerItem{
			Priority: event.Normal,
			Listener: event.ListenerFunc(um.preSiteUpdateHandler),
		},
		// BelowNormal: composer_audit writes the abandoned packages at Normal. At equal
		// priority the list would sometimes be read before it is written.
		"pre-merge-request-create": event.ListenerItem{
			Priority: event.BelowNormal,
			Listener: event.ListenerFunc(um.preMergeRequestCreateHandler),
		},
	}
}

// RenderTemplate renders modules and abandoned packages as one table, or nothing when empty.
func (um *UnsupportedModules) RenderTemplate() (string, error) {
	entries := um.endOfLifeEntries()
	if len(entries) == 0 {
		return "", nil
	}

	return um.Render("unsupported_modules.go.tmpl", entries)
}

// endOfLifeEntries returns the merged table rows. Sorted because both halves come from maps.
func (um *UnsupportedModules) endOfLifeEntries() []EndOfLifeEntry {
	entries := make([]EndOfLifeEntry, 0, len(um.modules)+len(um.abandoned))

	for _, module := range um.collectModules() {
		// drush reports "None" when there is no supported release — a status, not a version.
		recommendation := "Update to " + module.RecommendedVersion
		if module.RecommendedVersion == "" || module.RecommendedVersion == "None" {
			recommendation = "No supported release — replace it"
		}
		entries = append(entries, EndOfLifeEntry{
			Name:             module.Name,
			Status:           "Unsupported module",
			InstalledVersion: module.InstalledVersion,
			Recommendation:   recommendation,
		})
	}

	for _, pkg := range um.abandoned {
		recommendation := "No replacement suggested"
		if pkg.Replacement != "" {
			recommendation = "Replace with " + pkg.Replacement
		}
		entries = append(entries, EndOfLifeEntry{
			Name:           pkg.Name,
			Status:         "Abandoned package",
			Recommendation: recommendation,
		})
	}

	slices.SortStableFunc(entries, func(a, b EndOfLifeEntry) int {
		return strings.Compare(a.Name, b.Name)
	})

	return entries
}

// collectModules locks even though errgroup.Wait already orders every site's goroutine before
// rendering: that ordering is the caller's invariant, not this type's.
func (um *UnsupportedModules) collectModules() []drush.UnsupportedModule {
	um.mu.Lock()
	defer um.mu.Unlock()

	return slices.Collect(maps.Values(um.modules))
}

// preMergeRequestCreateHandler takes composer_audit's abandoned packages, to render alongside.
func (um *UnsupportedModules) preMergeRequestCreateHandler(e event.Event) error {
	evt := e.(*services.PreMergeRequestCreateEvent)

	um.abandoned = evt.AbandonedPackages

	return nil
}

// preSiteUpdateHandler checks a site. Best-effort: an unreachable status service is not an error.
func (um *UnsupportedModules) preSiteUpdateHandler(e event.Event) error {
	evt := e.(*services.PreSiteUpdateEvent)

	modules, err := um.drush.GetUnsupportedModules(evt.Context(), evt.Path(), evt.Site())
	if err != nil {
		um.logger.Warn("failed to check for unsupported modules", zap.String("site", evt.Site()), zap.Error(err))
		return nil
	}
	if len(modules) == 0 {
		return nil
	}

	um.mu.Lock()
	for _, module := range modules {
		um.modules[module.Name] = module
	}
	um.mu.Unlock()

	um.logger.Info("unsupported modules found", zap.String("site", evt.Site()), zap.Int("count", len(modules)))

	return nil
}
