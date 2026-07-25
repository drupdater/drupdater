package addon

import (
	"github.com/drupdater/drupdater/internal/services"
	"github.com/gookit/event"

	"github.com/go-git/go-git/v5"
	"go.uber.org/zap"
)

// DeprecationsRemover handles the removal of deprecated code using Rector
type DeprecationsRemover struct {
	logger   *zap.Logger
	rector   Rector
	composer Composer
}

// NewDeprecationsRemover creates a new deprecations remover instance
func NewDeprecationsRemover(logger *zap.Logger, rector Rector, composer Composer) *DeprecationsRemover {
	return &DeprecationsRemover{
		logger:   logger,
		rector:   rector,
		composer: composer,
	}
}

// SubscribedEvents returns the events this addon listens to
func (dr *DeprecationsRemover) SubscribedEvents() map[string]any {
	return map[string]any{
		// Above Normal: this handler temporarily requires palantirnet/drupal-rector (see below),
		// and code_beautifier's own "post-code-update" listener (Normal) may commit composer.*
		// via AddGlob if it installs drupal/coder — it must never run in between the require and
		// the cleanup this handler does before returning, or it would sweep up an unrelated,
		// half-finished composer.json/composer.lock diff into its own commit.
		"post-code-update": event.ListenerItem{
			Priority: event.AboveNormal,
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

	dr.logger.Info("removing deprecations")

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
		// Commit whatever composer.json/composer.lock diff remains from temporarily requiring
		// rector (a version-solving pass rarely restores the lock file byte-for-byte). Doing this
		// here, rather than leaving the files dirty, means no other "post-code-update" listener's
		// own worktree.AddGlob("composer.*") can ever sweep this unrelated diff into its commit.
		if err := dr.commitTemporaryRectorCleanup(evt.Worktree()); err != nil {
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

	dr.logger.Debug("committing deprecation removals")
	_, err = evt.Worktree().Commit("Remove deprecations", &git.CommitOptions{})

	return err
}

// commitTemporaryRectorCleanup commits any composer.json/composer.lock diff left over from
// temporarily requiring palantirnet/drupal-rector, or does nothing if removing it left no trace.
func (dr *DeprecationsRemover) commitTemporaryRectorCleanup(worktree Worktree) error {
	if err := worktree.AddGlob("composer.*"); err != nil {
		return err
	}
	staged, err := stagedAnyOf(worktree, []string{"composer.json", "composer.lock"})
	if err != nil {
		return err
	}
	if !staged {
		return nil
	}
	_, err = worktree.Commit("Remove temporary drupal-rector installation", &git.CommitOptions{})
	return err
}
