package services

import (
	"encoding/json"

	"github.com/drupdater/drupdater/internal"
	"github.com/drupdater/drupdater/internal/utils"

	"github.com/go-git/go-git/v5"
	"go.uber.org/zap"
)

type UpdateRemoveDeprecations struct {
	logger          *zap.Logger
	commandExecutor utils.CommandExecutor
	config          internal.Config
}

type RectorReturn struct {
	Totals struct {
		ChangedFiles int `json:"changed_files"`
		Errors       int `json:"errors"`
	} `json:"totals"`
	FileDiffs []struct {
		File           string   `json:"file"`
		Diff           string   `json:"diff"`
		AppliedRectors []string `json:"applied_rectors"`
	} `json:"file_diffs"`

	ChangedFiles []string `json:"changed_files"`
}

func newUpdateRemoveDeprecations(logger *zap.Logger, commandExecutor utils.CommandExecutor, config internal.Config) *UpdateRemoveDeprecations {
	return &UpdateRemoveDeprecations{
		logger:          logger,
		commandExecutor: commandExecutor,
		config:          config,
	}
}

func (h *UpdateRemoveDeprecations) Execute(path string, worktree internal.Worktree) error {
	if h.config.SkipRector {
		h.logger.Debug("rector is disabled, skipping remove deprecations")
		return nil
	}

	h.logger.Info("remove deprecations")

	// Check if rector is installed.
	installed, _ := h.commandExecutor.IsPackageInstalled(path, "palantirnet/drupal-rector")
	if !installed {
		h.logger.Debug("rector is not installed, installing")
		if _, err := h.commandExecutor.InstallPackages(path, "palantirnet/drupal-rector"); err != nil {
			return err
		}
	}

	out, err := h.commandExecutor.RunRector(path)
	if err != nil {
		return err
	}

	if !installed {
		h.logger.Debug("removing rector")
		if _, err := h.commandExecutor.RemovePackages(path, "palantirnet/drupal-rector"); err != nil {
			return err
		}
	}

	var deprecationRemovalResult RectorReturn
	if err := json.Unmarshal([]byte(out), &deprecationRemovalResult); err != nil {
		return err
	}

	if deprecationRemovalResult.Totals.ChangedFiles == 0 {
		h.logger.Debug("no deprecations to remove")
		return nil
	}

	for _, file := range deprecationRemovalResult.ChangedFiles {
		if _, err := worktree.Add(file); err != nil {
			return err
		}
	}

	h.logger.Debug("committing remove deprecations")
	_, err = worktree.Commit("Remove deprecations", &git.CommitOptions{})

	return err
}
