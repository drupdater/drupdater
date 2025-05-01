package addon

import (
	"fmt"

	"github.com/drupdater/drupdater/pkg/drush"
	"github.com/drupdater/drupdater/pkg/repo"
	"github.com/go-git/go-git/v5"
	"github.com/gookit/event"

	"go.uber.org/zap"
)

type UpdateTranslations struct {
	logger     *zap.Logger
	drush      drush.Runner
	repository repo.RepositoryService
}

func NewUpdateTranslations(logger *zap.Logger, drush drush.Runner, repository repo.RepositoryService) *UpdateTranslations {
	return &UpdateTranslations{
		logger:     logger,
		drush:      drush,
		repository: repository,
	}
}

func (h *UpdateTranslations) SubscribedEvents() map[string]interface{} {
	return map[string]interface{}{
		"post-code-update": event.ListenerItem{
			Priority: event.Normal,
			Listener: event.ListenerFunc(h.postSiteUpdateHandler),
		},
	}
}

func (h *UpdateTranslations) postSiteUpdateHandler(e event.Event) error {

	event := e.(*PostSiteUpdate)

	enabled, err := h.drush.IsModuleEnabled(event.Ctx, event.Path, event.Site, "locale_deploy")
	if !enabled || err != nil {
		return err
	}

	h.logger.Info("updating translations")

	if err := h.drush.LocalizeTranslations(event.Ctx, event.Path, event.Site); err != nil {
		return err
	}

	translationPath, err := h.drush.GetTranslationPath(event.Ctx, event.Path, event.Site, true)
	if err != nil {
		return err
	}

	_, err = event.Worktree.Add(translationPath)
	if err != nil {
		return fmt.Errorf("failed to add translation path: %w", err)
	}

	status, _ := event.Worktree.Status()
	h.logger.Debug("Git status", zap.Any("status", status))
	if !h.repository.IsSomethingStagedInPath(event.Worktree, translationPath) {
		h.logger.Debug("nothing to commit")
		return nil
	}
	_, err = event.Worktree.Commit("Update translations", &git.CommitOptions{})
	if err != nil {
		return fmt.Errorf("failed to commit translation path: %w", err)
	}

	return nil
}
