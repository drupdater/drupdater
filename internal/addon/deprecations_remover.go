package addon

import (
	"cmp"
	"slices"

	"github.com/drupdater/drupdater/internal/workflow"
	"github.com/drupdater/drupdater/pkg/rector"

	"go.uber.org/zap"
)

type DeprecationsRemover struct {
	logger   *zap.Logger
	rector   Rector
	composer Composer

	// For the report. Written once from the single post-code-update event, read after the run.
	fixes []DeprecationFix
}

func NewDeprecationsRemover(logger *zap.Logger, rector Rector, composer Composer) *DeprecationsRemover {
	return &DeprecationsRemover{
		logger:   logger,
		rector:   rector,
		composer: composer,
	}
}

func (dr *DeprecationsRemover) Hooks() workflow.Hooks {
	return workflow.Hooks{
		PostCodeUpdate: workflow.On(workflow.Change, dr.postCodeUpdateHandler),
	}
}

func (dr *DeprecationsRemover) RenderTemplate() (string, error) {
	return "", nil
}

func (dr *DeprecationsRemover) postCodeUpdateHandler(evt *workflow.PostCodeUpdateEvent) error {

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
		// Removing rector rarely restores composer.lock byte-for-byte; the remainder gets its own commit.
		if _, err := evt.Commit("Remove temporary drupal-rector installation", "composer.json", "composer.lock"); err != nil {
			return err
		}
	}

	if deprecationRemovalResult.Totals.ChangedFiles == 0 {
		dr.logger.Debug("no deprecations to remove")
		return nil
	}

	dr.recordFixes(deprecationRemovalResult)

	dr.logger.Debug("committing deprecation removals")
	_, err = evt.Commit("Remove deprecations", deprecationRemovalResult.ChangedFiles...)
	return err
}

// recordFixes captures which rules fired on which files, for the report.
func (dr *DeprecationsRemover) recordFixes(result rector.ReturnOutput) {
	rectorsByFile := make(map[string][]string, len(result.FileDiffs))
	for _, diff := range result.FileDiffs {
		rectorsByFile[diff.File] = diff.AppliedRectors
	}

	fixes := make([]DeprecationFix, 0, len(result.ChangedFiles))
	for _, file := range result.ChangedFiles {
		// Cloned: sorting in place would reorder the caller's rector.ReturnOutput.
		applied := slices.Clone(rectorsByFile[file])
		slices.Sort(applied)
		fixes = append(fixes, DeprecationFix{File: file, AppliedRectors: applied})
	}
	// Sorted so two runs over unchanged code produce byte-identical sections.
	slices.SortFunc(fixes, func(a, b DeprecationFix) int { return cmp.Compare(a.File, b.File) })
	dr.fixes = fixes
}
