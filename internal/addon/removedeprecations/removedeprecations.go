package removedeprecations

import (
	"github.com/drupdater/drupdater/internal"
	"github.com/drupdater/drupdater/internal/addon"
	"github.com/drupdater/drupdater/pkg/composer"
	"github.com/drupdater/drupdater/pkg/rector"
	"github.com/gookit/event"

	"github.com/go-git/go-git/v5"
	"go.uber.org/zap"
)

type UpdateRemoveDeprecations struct {
	logger   *zap.Logger
	rector   rector.Runner
	config   internal.Config
	composer composer.Runner
}

func NewUpdateRemoveDeprecations(logger *zap.Logger, rector rector.Runner, config internal.Config, composer composer.Runner) *UpdateRemoveDeprecations {
	return &UpdateRemoveDeprecations{
		logger:   logger,
		rector:   rector,
		config:   config,
		composer: composer,
	}
}

func (h *UpdateRemoveDeprecations) SubscribedEvents() map[string]interface{} {
	return map[string]interface{}{
		"post-code-update": event.ListenerItem{
			Priority: event.Normal,
			Listener: event.ListenerFunc(h.postCodeUpdateHandler),
		},
	}
}

func (h *UpdateRemoveDeprecations) RenderTemplate() (string, error) {
	return "", nil
}

func (h *UpdateRemoveDeprecations) postCodeUpdateHandler(e event.Event) error {

	event := e.(*addon.PostCodeUpdateEvent)

	h.logger.Info("remove deprecations")

	// Check if rector is installed.
	installed, _ := h.composer.IsPackageInstalled(event.Ctx, event.Path, "palantirnet/drupal-rector")
	if !installed {
		h.logger.Debug("rector is not installed, installing")
		if _, err := h.composer.Require(event.Ctx, event.Path, "palantirnet/drupal-rector"); err != nil {
			return err
		}
	}

	customCodeDirectories, err := h.composer.GetCustomCodeDirectories(event.Ctx, event.Path)
	if err != nil {
		return err
	}

	deprecationRemovalResult, err := h.rector.Run(event.Ctx, event.Path, customCodeDirectories)
	if err != nil {
		return err
	}

	if !installed {
		h.logger.Debug("removing rector")
		if _, err := h.composer.Remove(event.Ctx, event.Path, "palantirnet/drupal-rector"); err != nil {
			return err
		}
	}

	if deprecationRemovalResult.Totals.ChangedFiles == 0 {
		h.logger.Debug("no deprecations to remove")
		return nil
	}

	for _, file := range deprecationRemovalResult.ChangedFiles {
		if _, err := event.Worktree.Add(file); err != nil {
			return err
		}
	}

	h.logger.Debug("committing remove deprecations")
	_, err = event.Worktree.Commit("Remove deprecations", &git.CommitOptions{})

	return err
}
