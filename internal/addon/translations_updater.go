package addon

import (
	"fmt"
	"sync"

	"github.com/drupdater/drupdater/internal/services"
	"github.com/go-git/go-git/v5"
	"github.com/gookit/event"

	"go.uber.org/zap"
)

// TranslationsUpdater handles updating translations for Drupal sites
type TranslationsUpdater struct {
	logger     *zap.Logger
	drush      Drush
	repository Repository

	// Per-site outcome for the report. post-site-update fires concurrently across sites,
	// hence the mutex.
	mu      sync.Mutex
	results map[string]TranslationResult
}

// NewTranslationsUpdater creates a new translations updater instance
func NewTranslationsUpdater(logger *zap.Logger, drush Drush, repository Repository) *TranslationsUpdater {
	return &TranslationsUpdater{
		logger:     logger,
		drush:      drush,
		repository: repository,
	}
}

// SubscribedEvents returns the events this addon listens to
func (tu *TranslationsUpdater) SubscribedEvents() map[string]any {
	return map[string]any{
		"post-site-update": event.ListenerItem{
			Priority: event.Normal,
			Listener: event.ListenerFunc(tu.postSiteUpdateHandler),
		},
	}
}

// RenderTemplate returns the rendered template for this addon
func (tu *TranslationsUpdater) RenderTemplate() (string, error) {
	return "", nil
}

func (tu *TranslationsUpdater) postSiteUpdateHandler(e event.Event) error {
	evt := e.(*services.PostSiteUpdateEvent)

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

	_, err = evt.Worktree().Add(translationPath)
	if err != nil {
		return fmt.Errorf("failed to add translation path: %w", err)
	}

	status, _ := evt.Worktree().Status()
	tu.logger.Debug("git status", zap.Any("status", status))
	if !tu.repository.IsSomethingStagedInPath(evt.Worktree(), translationPath) {
		tu.logger.Debug("nothing to commit")
		tu.record(evt.Site(), TranslationResult{Path: translationPath})
		return nil
	}
	_, err = evt.Worktree().Commit("Update translations", &git.CommitOptions{})
	if err != nil {
		return fmt.Errorf("failed to commit translation path: %w", err)
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
