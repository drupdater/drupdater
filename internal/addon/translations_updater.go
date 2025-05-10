package addon

import (
	"fmt"

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
func (tu *TranslationsUpdater) SubscribedEvents() map[string]interface{} {
	return map[string]interface{}{
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
	if !enabled || err != nil {
		return err
	}

	tu.logger.Info("updating translations")

	if err := tu.drush.LocalizeTranslations(evt.Context(), evt.Path(), evt.Site()); err != nil {
		return err
	}

	translationPath, err := tu.drush.GetTranslationPath(evt.Context(), evt.Path(), evt.Site(), true)
	if err != nil {
		return err
	}

	_, err = evt.Worktree().Add(translationPath)
	if err != nil {
		return fmt.Errorf("failed to add translation path: %w", err)
	}

	status, _ := evt.Worktree().Status()
	tu.logger.Debug("Git status", zap.Any("status", status))
	if !tu.repository.IsSomethingStagedInPath(evt.Worktree(), translationPath) {
		tu.logger.Debug("nothing to commit")
		return nil
	}
	_, err = evt.Worktree().Commit("Update translations", &git.CommitOptions{})
	if err != nil {
		return fmt.Errorf("failed to commit translation path: %w", err)
	}

	return nil
}
