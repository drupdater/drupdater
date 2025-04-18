package services

import (
	"context"
	"encoding/json"

	"github.com/drupdater/drupdater/internal"
	"github.com/drupdater/drupdater/pkg/composer"
	"github.com/drupdater/drupdater/pkg/rector"

	"github.com/go-git/go-git/v5"
	"go.uber.org/zap"
)

type UpdateRemoveDeprecations struct {
	logger   *zap.Logger
	rector   rector.Runner
	config   internal.Config
	composer composer.Runner
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

func newUpdateRemoveDeprecations(logger *zap.Logger, rector rector.Runner, config internal.Config, composer composer.Runner) *UpdateRemoveDeprecations {
	return &UpdateRemoveDeprecations{
		logger:   logger,
		rector:   rector,
		config:   config,
		composer: composer,
	}
}

func (h *UpdateRemoveDeprecations) Execute(ctx context.Context, path string, worktree internal.Worktree) error {
	if h.config.SkipRector {
		h.logger.Debug("rector is disabled, skipping remove deprecations")
		return nil
	}

	h.logger.Info("remove deprecations")

	// Check if rector is installed.
	installed, _ := h.composer.IsPackageInstalled(ctx, path, "palantirnet/drupal-rector")
	if !installed {
		h.logger.Debug("rector is not installed, installing")
		if _, err := h.composer.Require(ctx, path, "palantirnet/drupal-rector"); err != nil {
			return err
		}
	}

	customCodeDirectories, err := h.composer.GetCustomCodeDirectories(ctx, path)
	if err != nil {
		return err
	}

	out, err := h.rector.Run(ctx, path, customCodeDirectories)
	if err != nil {
		return err
	}

	if !installed {
		h.logger.Debug("removing rector")
		if _, err := h.composer.Remove(ctx, path, "palantirnet/drupal-rector"); err != nil {
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
