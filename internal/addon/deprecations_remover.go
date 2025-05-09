package addon

import (
	"github.com/drupdater/drupdater/internal"
	"github.com/drupdater/drupdater/internal/services"
	"github.com/gookit/event"

	"github.com/go-git/go-git/v5"
	"go.uber.org/zap"
)

// DeprecationsRemover handles the removal of deprecated code using Rector
type DeprecationsRemover struct {
	logger   *zap.Logger
	rector   Rector
	config   internal.Config
	composer Composer
}

// NewDeprecationsRemover creates a new deprecations remover instance
func NewDeprecationsRemover(logger *zap.Logger, rector Rector, config internal.Config, composer Composer) *DeprecationsRemover {
	return &DeprecationsRemover{
		logger:   logger,
		rector:   rector,
		config:   config,
		composer: composer,
	}
}

// SubscribedEvents returns the events this addon listens to
func (dr *DeprecationsRemover) SubscribedEvents() map[string]interface{} {
	return map[string]interface{}{
		"post-code-update": event.ListenerItem{
			Priority: event.Normal,
			Listener: event.ListenerFunc(dr.postCodeUpdateHandler),
		},
	}
}

// RenderTemplate returns the rendered template for this addon
func (dr *DeprecationsRemover) RenderTemplate() (string, error) {
	return "", nil
}

func (dr *DeprecationsRemover) postCodeUpdateHandler(e event.Event) error {
	evt := e.(*services.PostCodeUpdateEvent)

	dr.logger.Info("remove deprecations")

	// Check if rector is installed.
	installed, _ := dr.composer.IsPackageInstalled(evt.Context(), evt.Path(), "palantirnet/drupal-rector")
	if !installed {
		dr.logger.Debug("rector is not installed, installing")
		if _, err := dr.composer.Require(evt.Context(), evt.Path(), "palantirnet/drupal-rector"); err != nil {
			return err
		}
	}

	customCodeDirectories, err := dr.composer.GetCustomCodeDirectories(evt.Context(), evt.Path())
	if err != nil {
		return err
	}

	deprecationRemovalResult, err := dr.rector.Run(evt.Context(), evt.Path(), customCodeDirectories)
	if err != nil {
		return err
	}

	if !installed {
		dr.logger.Debug("removing rector")
		if _, err := dr.composer.Remove(evt.Context(), evt.Path(), "palantirnet/drupal-rector"); err != nil {
			return err
		}
	}

	if deprecationRemovalResult.Totals.ChangedFiles == 0 {
		dr.logger.Debug("no deprecations to remove")
		return nil
	}

	for _, file := range deprecationRemovalResult.ChangedFiles {
		if _, err := evt.Worktree().Add(file); err != nil {
			return err
		}
	}

	dr.logger.Debug("committing remove deprecations")
	_, err = evt.Worktree().Commit("Remove deprecations", &git.CommitOptions{})

	return err
}
