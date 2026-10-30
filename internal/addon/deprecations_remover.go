package addon

import (
	"cmp"
	"context"
	"slices"

	"github.com/drupdater/drupdater/internal/workflow"
	"github.com/drupdater/drupdater/pkg/rector"

	"go.uber.org/zap"
)

// DeprecationsComposer finds custom code and whether the project ships its own Rector.
type DeprecationsComposer interface {
	GetCustomCodeDirectories(ctx context.Context, dir string) ([]string, error)
	IsPackageInstalled(ctx context.Context, dir string, packageToCheck string) (bool, error)
}

// Rector runs rector over custom code.
type Rector interface {
	Run(ctx context.Context, dir string, customCodeDirectories []string) (rector.ReturnOutput, error)
}

type DeprecationsRemover struct {
	logger        *zap.Logger
	projectRector Rector
	bundledRector Rector
	composer      DeprecationsComposer

	// For the report. Written once from the single post-code-update event, read after the run.
	fixes []DeprecationFix
}

// NewDeprecationsRemover runs projectRector when the project has drupal-rector, bundledRector
// when it has no Rector at all.
func NewDeprecationsRemover(logger *zap.Logger, projectRector Rector, bundledRector Rector, composer DeprecationsComposer) *DeprecationsRemover {
	return &DeprecationsRemover{
		logger:        logger,
		projectRector: projectRector,
		bundledRector: bundledRector,
		composer:      composer,
	}
}

func (dr *DeprecationsRemover) Hooks() workflow.Hooks {
	return workflow.Hooks{
		PostCodeUpdate: workflow.On(workflow.Change, dr.postCodeUpdateHandler),
	}
}

func (dr *DeprecationsRemover) postCodeUpdateHandler(evt *workflow.PostCodeUpdateEvent) error {
	dr.logger.Info("removing deprecations")

	rector := dr.rectorFor(evt.Context(), evt.Path())
	if rector == nil {
		return nil
	}

	customCodeDirectories, err := dr.composer.GetCustomCodeDirectories(evt.Context(), evt.Path())
	if err != nil {
		return err
	}

	deprecationRemovalResult, err := rector.Run(evt.Context(), evt.Path(), customCodeDirectories)
	if err != nil {
		return err
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

// rectorFor returns the Rector that can load the project's code, or nil when neither can.
func (dr *DeprecationsRemover) rectorFor(ctx context.Context, path string) Rector {
	if installed, _ := dr.composer.IsPackageInstalled(ctx, path, "palantirnet/drupal-rector"); installed {
		return dr.projectRector
	}
	// Ours would load the project's autoloader, and with it a second Rector's classes.
	if installed, _ := dr.composer.IsPackageInstalled(ctx, path, "rector/rector"); installed {
		dr.logger.Warn("skipping deprecation removal: the project requires rector/rector but not palantirnet/drupal-rector")
		return nil
	}
	return dr.bundledRector
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
