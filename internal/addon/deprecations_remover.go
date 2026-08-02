package addon

import (
	"cmp"
	"slices"

	"github.com/drupdater/drupdater/internal/services"
	"github.com/drupdater/drupdater/pkg/rector"
	"github.com/gookit/event"

	"github.com/go-git/go-git/v5"
	"go.uber.org/zap"
)

type DeprecationsRemover struct {
	logger   *zap.Logger
	rector   Rector
	composer Composer

	// Which rules rewrote which files, for the report. Written once from the single
	// post-code-update event, read after the run.
	fixes []DeprecationFix
}

func NewDeprecationsRemover(logger *zap.Logger, rector Rector, composer Composer) *DeprecationsRemover {
	return &DeprecationsRemover{
		logger:   logger,
		rector:   rector,
		composer: composer,
	}
}

func (dr *DeprecationsRemover) SubscribedEvents() map[string]any {
	return map[string]any{
		// Above Normal: this handler temporarily requires palantirnet/drupal-rector, and
		// code_beautifier (Normal) may commit composer.* via AddGlob. Running in between would
		// sweep this handler's half-finished composer diff into that commit.
		"post-code-update": event.ListenerItem{
			Priority: event.AboveNormal,
			Listener: event.ListenerFunc(dr.postCodeUpdateHandler),
		},
	}
}

func (dr *DeprecationsRemover) RenderTemplate() (string, error) {
	return "", nil
}

func (dr *DeprecationsRemover) postCodeUpdateHandler(e event.Event) error {
	evt := e.(*services.PostCodeUpdateEvent)

	dr.logger.Info("removing deprecations")

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
		// Removing rector rarely restores composer.lock byte-for-byte. Commit the remainder here
		// so no other listener's AddGlob("composer.*") sweeps this diff into its own commit.
		if err := dr.commitTemporaryRectorCleanup(evt.Worktree()); err != nil {
			return err
		}
	}

	if deprecationRemovalResult.Totals.ChangedFiles == 0 {
		dr.logger.Debug("no deprecations to remove")
		return nil
	}

	dr.recordFixes(deprecationRemovalResult)

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

// recordFixes captures which rules fired on which files, so the report says more than "some
// deprecations were removed".
func (dr *DeprecationsRemover) recordFixes(result rector.ReturnOutput) {
	rectorsByFile := make(map[string][]string, len(result.FileDiffs))
	for _, diff := range result.FileDiffs {
		rectorsByFile[diff.File] = diff.AppliedRectors
	}

	fixes := make([]DeprecationFix, 0, len(result.ChangedFiles))
	for _, file := range result.ChangedFiles {
		// Cloned before sorting: in place would reorder the caller's rector.ReturnOutput, and a
		// file listed twice would leave two fixes sharing one backing array.
		applied := slices.Clone(rectorsByFile[file])
		slices.Sort(applied)
		fixes = append(fixes, DeprecationFix{File: file, AppliedRectors: applied})
	}
	// Sorted so two runs over unchanged code produce byte-identical sections.
	slices.SortFunc(fixes, func(a, b DeprecationFix) int { return cmp.Compare(a.File, b.File) })
	dr.fixes = fixes
}
