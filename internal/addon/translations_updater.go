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
}

func NewTranslationsUpdater(logger *zap.Logger, drush TranslationsDrush) *TranslationsUpdater {
	return &TranslationsUpdater{
		logger: logger,
		drush:  drush,
	}
}

func (tu *TranslationsUpdater) Hooks() workflow.Hooks {
	return workflow.Hooks{
		PostSiteUpdate: workflow.Produce(workflow.Change, tu.postSiteUpdateHandler),
	}
}

// postSiteUpdateHandler leaves Updated to ReportData, which knows whether the commit was made.
func (tu *TranslationsUpdater) postSiteUpdateHandler(evt *workflow.PostSiteUpdateEvent) (workflow.Result[TranslationResult], error) {
	facts := evt.Facts()
	if !facts.ModuleEnabled("locale_deploy") {
		tu.logger.Info("locale_deploy not enabled, skipping translations update", zap.String("site", evt.Site()))
		return translationsSkipped("locale_deploy not enabled"), nil
	}

	tu.logger.Info("updating translations")

	if err := tu.drush.LocalizeTranslations(evt.Context(), evt.Path(), evt.Site()); err != nil {
		return workflow.Result[TranslationResult]{}, err
	}

	translationPath, err := existingDir(facts.TranslationPath)
	if err != nil {
		tu.logger.Info("translation path not available, skipping translations update", zap.String("site", evt.Site()), zap.Error(err))
		return translationsSkipped("translation path not available: " + err.Error()), nil
	}
	translationPath = drupal.RelativeTo(evt.Path(), translationPath)

	return workflow.Result[TranslationResult]{
		Commits: []workflow.Commit{{Message: translationsCommitMessage, Paths: []string{translationPath}}},
		Finding: TranslationResult{Path: translationPath},
	}, nil
}

const translationsCommitMessage = "Update translations"

func translationsSkipped(reason string) workflow.Result[TranslationResult] {
	return workflow.Result[TranslationResult]{Finding: TranslationResult{Skipped: reason}}
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
