package addon

import (
	"context"
	"fmt"
	"strings"

	"github.com/drupdater/drupdater/internal/composer"
	"github.com/drupdater/drupdater/internal/workflow"
	"go.uber.org/zap"
)

// DiffComposer diffs the lock file against the one the run started from.
type DiffComposer interface {
	Diff(ctx context.Context, path string) ([]composer.DiffEntry, error)
}

type ComposerDiff struct {
	logger   *zap.Logger
	composer DiffComposer
}

func NewComposerDiff(logger *zap.Logger, composer DiffComposer) *ComposerDiff {
	return &ComposerDiff{
		logger:   logger,
		composer: composer,
	}
}

func (cd *ComposerDiff) Hooks() workflow.Hooks {
	return workflow.Hooks{
		PostComposerUpdate: workflow.Observe(cd.postComposerUpdateHandler),
	}
}

func (cd *ComposerDiff) postComposerUpdateHandler(evt *workflow.PostComposerUpdateEvent) ([]composer.DiffEntry, error) {
	entries, err := cd.composer.Diff(evt.Context(), evt.Path())
	if err != nil {
		return nil, fmt.Errorf("failed to get diff: %w", err)
	}

	if len(entries) > 0 {
		cd.logger.Info("dependency diff\n" + plainDiff(entries))
	}

	return entries, nil
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
