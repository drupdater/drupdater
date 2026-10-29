package addon

import (
	"fmt"
	"sync"

	"github.com/drupdater/drupdater/internal/workflow"
	"github.com/gookit/event"

	"go.uber.org/zap"
)

type TranslationsUpdater struct {
	logger *zap.Logger
	drush  Drush

	// Per-site outcome for the report. post-site-update fires concurrently, hence the mutex.
	mu      sync.Mutex
	results map[string]TranslationResult
}

func NewTranslationsUpdater(logger *zap.Logger, drush Drush) *TranslationsUpdater {
	return &TranslationsUpdater{
		logger: logger,
		drush:  drush,
	}
}

func (tu *TranslationsUpdater) SubscribedEvents() map[string]any {
	return map[string]any{
		"post-site-update": event.ListenerItem{
			Priority: event.Normal,
			Listener: event.ListenerFunc(tu.postSiteUpdateHandler),
		},
	}
}

func (tu *TranslationsUpdater) RenderTemplate() (string, error) {
	return "", nil
}

func (tu *TranslationsUpdater) postSiteUpdateHandler(e event.Event) error {
	evt := e.(*workflow.PostSiteUpdateEvent)

	enabled, err := tu.drush.IsModuleEnabled(evt.Context(), evt.Path(), evt.Site(), "locale_deploy")
	if err != nil {
		return err
	}
	if !enabled {
		tu.logger.Info("locale_deploy not enabled, skipping translations update", zap.String("site", evt.Site()))
		tu.record(evt.Site(), TranslationResult{Skipped: "locale_deploy not enabled"})
		return nil
	}

	tu.logger.Info("updating translations")

	if err := tu.drush.LocalizeTranslations(evt.Context(), evt.Path(), evt.Site()); err != nil {
		return err
	}

	translationPath, err := tu.drush.GetTranslationPath(evt.Context(), evt.Path(), evt.Site(), true)
	if err != nil {
		tu.logger.Info("translation path not available, skipping translations update", zap.String("site", evt.Site()), zap.Error(err))
		tu.record(evt.Site(), TranslationResult{Skipped: "translation path not available: " + err.Error()})
		return nil
	}

	committed, err := evt.Commit("Update translations", translationPath)
	if err != nil {
		return fmt.Errorf("failed to commit translation path: %w", err)
	}
	if !committed {
		tu.logger.Debug("nothing to commit")
		tu.record(evt.Site(), TranslationResult{Path: translationPath})
		return nil
	}

	tu.record(evt.Site(), TranslationResult{Path: translationPath, Updated: true})
	return nil
}

// record stores a site's outcome for the report.
func (tu *TranslationsUpdater) record(site string, result TranslationResult) {
	tu.mu.Lock()
	defer tu.mu.Unlock()
	if tu.results == nil {
		tu.results = make(map[string]TranslationResult)
	}
	tu.results[site] = result
}
