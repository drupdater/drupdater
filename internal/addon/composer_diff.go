package addon

import (
	"fmt"
	"strings"

	"github.com/drupdater/drupdater/internal/workflow"
	"github.com/drupdater/drupdater/pkg/composer"
	"go.uber.org/zap"
)

type ComposerDiff struct {
	logger   *zap.Logger
	composer Composer

	entries []composer.DiffEntry
}

func NewComposerDiff(logger *zap.Logger, composer Composer) *ComposerDiff {
	return &ComposerDiff{
		logger:   logger,
		composer: composer,
	}
}

func (cd *ComposerDiff) Hooks() workflow.Hooks {
	return workflow.Hooks{
		PostComposerUpdate: workflow.On(workflow.Inspect, cd.postComposerUpdateHandler),
	}
}

func (cd *ComposerDiff) postComposerUpdateHandler(evt *workflow.PostComposerUpdateEvent) error {
	entries, err := cd.composer.Diff(evt.Context(), evt.Path())
	if err != nil {
		return fmt.Errorf("failed to get diff: %w", err)
	}
	cd.entries = entries

	if len(entries) > 0 {
		cd.logger.Info("dependency diff\n" + plainDiff(entries))
	}

	return nil
}

// plainDiff is the log's rendering: one line per package, without the links a terminal can't use.
func plainDiff(entries []composer.DiffEntry) string {
	var b strings.Builder
	for _, e := range entries {
		dev := ""
		if e.Dev {
			dev = " (dev)"
		}
		fmt.Fprintf(&b, "%s %s%s: %s → %s\n", e.Operation, e.Package, dev, orDash(e.From), orDash(e.To))
	}
	return b.String()
}

func orDash(version string) string {
	if version == "" {
		return "-"
	}
	return version
}
