package services

import (
	"context"

	"github.com/drupdater/drupdater/internal"
	"github.com/drupdater/drupdater/internal/utils"

	git "github.com/go-git/go-git/v5"
	"go.uber.org/zap"
)

type UpdateTranslations struct {
	logger          *zap.Logger
	commandExecutor utils.CommandExecutor
	repository      RepositoryService
}

func newUpdateTranslations(logger *zap.Logger, commandExecutor utils.CommandExecutor, repository RepositoryService) *UpdateTranslations {
	return &UpdateTranslations{
		logger:          logger,
		commandExecutor: commandExecutor,
		repository:      repository,
	}
}

func (h *UpdateTranslations) Execute(ctx context.Context, path string, worktree internal.Worktree, site string) error {
	enabled, err := h.commandExecutor.IsModuleEnabled(ctx, path, site, "locale_deploy")
	if !enabled || err != nil {
		return err
	}

	h.logger.Info("updating translations")

	if err := h.commandExecutor.LocalizeTranslations(ctx, path, site); err != nil {
		return err
	}

	translationPath, err := h.commandExecutor.GetTranslationPath(ctx, path, site, true)
	if err != nil {
		return err
	}

	_, err = worktree.Add(translationPath)
	if err != nil {
		h.logger.Error("failed to add translation path", zap.Error(err), zap.String("site", site))
	}

	status, _ := worktree.Status()
	h.logger.Debug("Git status", zap.Any("status", status))
	if !h.repository.IsSomethingStagedInPath(worktree, translationPath) {
		h.logger.Debug("nothing to commit")
		return nil
	}
	_, err = worktree.Commit("Update translations", &git.CommitOptions{})
	if err != nil {
		h.logger.Error("failed to commit translation path", zap.Error(err), zap.String("site", site))
	}

	return nil
}
