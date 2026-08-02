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

// UnsupportedModules detects installed modules that have reached end-of-life according to
// Drupal's update status service (status NOT_SUPPORTED): they have no supported upgrade path,
// which is a different risk category than a security vulnerability caught by composer_audit.
//
// It also renders the abandoned packages composer_audit hands it on pre-merge-request-create.
// Those come from Packagist rather than drupal.org and cover the non-Drupal libraries this
// addon cannot see, but they say the same thing to a reviewer — no further fixes are coming,
// decide what to do — so they belong in one list rather than in two sections that differ only
// by which addon found them.
type UnsupportedModules struct {
	internal.BasicAddon
	logger *zap.Logger
	drush  Drush

	// mu guards modules: preSiteUpdateHandler runs concurrently for each site. Keyed by module
	// name so results are deduplicated across sites in multisite runs.
	mu      sync.Mutex
	modules map[string]drush.UnsupportedModule

	// abandoned is written once, from pre-merge-request-create, which fires after every site's
	// goroutine has finished — so unlike modules it needs no lock.
	abandoned []services.AbandonedPackage
}

// EndOfLifeEntry is one row of the merged "unsupported or abandoned" table. The two sources
// answer different questions, so the row keeps a Status naming which one it came from rather
// than pretending a module version and a replacement package are the same column.
type EndOfLifeEntry struct {
	Name string
	// Status is "Unsupported module" or "Abandoned package".
	Status string
	// InstalledVersion is empty for an abandoned package: `composer audit` does not report one,
	// and the dependency table already carries every installed version.
	InstalledVersion string
	// Recommendation is what to do about it, already phrased for the reader.
	Recommendation string
}

// NewUnsupportedModules creates a new unsupported modules detector instance.
func NewUnsupportedModules(logger *zap.Logger, drushClient Drush) *UnsupportedModules {
	return &UnsupportedModules{
		logger:  logger,
		drush:   drushClient,
		modules: make(map[string]drush.UnsupportedModule),
	}
}

// SubscribedEvents returns the events this addon listens to.
func (um *UnsupportedModules) SubscribedEvents() map[string]any {
	return map[string]any{
		"pre-site-update": event.ListenerItem{
			Priority: event.Normal,
			Listener: event.ListenerFunc(um.preSiteUpdateHandler),
		},
		// Below Normal: composer_audit puts the abandoned packages on this event at Normal, and
		// this addon renders them. Running at the same priority would leave which of the two
		// goes first to the dispatcher, and the list would sometimes be read before it is
		// written.
		"pre-merge-request-create": event.ListenerItem{
			Priority: event.BelowNormal,
			Listener: event.ListenerFunc(um.preMergeRequestCreateHandler),
		},
	}
}

// RenderTemplate returns the rendered template for this addon: the unsupported modules and the
// abandoned packages as one table, or nothing at all when there is neither.
func (um *UnsupportedModules) RenderTemplate() (string, error) {
	entries := um.endOfLifeEntries()
	if len(entries) == 0 {
		return "", nil
	}

	return um.Render("unsupported_modules.go.tmpl", entries)
}

// endOfLifeEntries returns the merged table rows, sorted by name — one list rather than two
// groups, because the Status column already says which kind each row is and a reviewer looking
// for a particular package should not have to know which addon found it first.
//
// Sorting is also what makes the section byte-stable: both halves originate from maps, whose
// iteration order is random, so an unsorted table would differ between two runs over an
// unchanged project.
func (um *UnsupportedModules) endOfLifeEntries() []EndOfLifeEntry {
	entries := make([]EndOfLifeEntry, 0, len(um.modules)+len(um.abandoned))

	for _, module := range um.collectModules() {
		// drush reports the literal string "None" when there is no supported release at all,
		// which is a status rather than a version to move to.
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

// collectModules returns the modules gathered so far.
//
// The lock is not required for correctness today — the workflow only renders after every site's
// forEachSite goroutine has returned, and errgroup.Wait establishes a happens-before edge for
// that — but that ordering is an invariant of the caller, not of this type, so mu still guards
// the read.
func (um *UnsupportedModules) collectModules() []drush.UnsupportedModule {
	um.mu.Lock()
	defer um.mu.Unlock()

	return slices.Collect(maps.Values(um.modules))
}

// preMergeRequestCreateHandler takes the abandoned packages composer_audit found so they can be
// rendered alongside the unsupported modules. It does not log them: composer_audit already
// logged the count when it ran the audit that produced them.
func (um *UnsupportedModules) preMergeRequestCreateHandler(e event.Event) error {
	evt := e.(*services.PreMergeRequestCreateEvent)

	um.abandoned = evt.AbandonedPackages

	return nil
}

// preSiteUpdateHandler checks a site for unsupported modules. This is a best-effort, informational
// check: failures (e.g. the update status service being unreachable) are logged and swallowed
// rather than aborting the run, since an unsupported module is reported, not treated as an error.
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
