package addon

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/drupdater/drupdater/internal/drupal"
	"github.com/drupdater/drupdater/internal/workflow"

	"go.uber.org/zap"
)

// TranslationsDrush imports a site's translations.
type TranslationsDrush interface {
	LocalizeTranslations(ctx context.Context, dir string, site string) error
}

type TranslationsUpdater struct {
	logger *zap.Logger
	drush  TranslationsDrush

	results workflow.PerSite[TranslationResult]
}

func NewTranslationsUpdater(logger *zap.Logger, drush TranslationsDrush) *TranslationsUpdater {
	return &TranslationsUpdater{
		logger: logger,
		drush:  drush,
	}
}

func (tu *TranslationsUpdater) Hooks() workflow.Hooks {
	return workflow.Hooks{
		PostSiteUpdate: workflow.On(workflow.Change, tu.postSiteUpdateHandler),
	}
}

func (tu *TranslationsUpdater) postSiteUpdateHandler(evt *workflow.PostSiteUpdateEvent) error {

	facts := evt.Facts()
	if !facts.ModuleEnabled("locale_deploy") {
		tu.logger.Info("locale_deploy not enabled, skipping translations update", zap.String("site", evt.Site()))
		tu.record(evt.Site(), TranslationResult{Skipped: "locale_deploy not enabled"})
		return nil
	}

	tu.logger.Info("updating translations")

	if err := tu.drush.LocalizeTranslations(evt.Context(), evt.Path(), evt.Site()); err != nil {
		return err
	}

	translationPath, err := existingDir(facts.TranslationPath)
	if err != nil {
		tu.logger.Info("translation path not available, skipping translations update", zap.String("site", evt.Site()), zap.Error(err))
		tu.record(evt.Site(), TranslationResult{Skipped: "translation path not available: " + err.Error()})
		return nil
	}
	translationPath = drupal.RelativeTo(evt.Path(), translationPath)

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

// existingDir refuses an empty path, which would reach git as the whole working tree.
func existingDir(path string) (string, error) {
	if path == "" {
		return "", errors.New("no translation path is configured")
	}
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		return "", fmt.Errorf("%s is not an existing directory", path)
	}
	return path, nil
}

// record stores a site's outcome for the report.
func (tu *TranslationsUpdater) record(site string, result TranslationResult) {
	tu.results.Set(site, result)
}
