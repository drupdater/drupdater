package services

import (
	"context"

	"github.com/drupdater/drupdater/internal/report"
	composerpkg "github.com/drupdater/drupdater/pkg/composer"
	"go.uber.org/zap"
)

// ComposerVersionReporter is all LookupToolVersions needs, narrow like preflight.go's checkers.
type ComposerVersionReporter interface {
	Version(ctx context.Context) (composerpkg.Versions, error)
}

// LookupToolVersions reads the versions for the report and logs them, so a run without --report
// names them too. A failure warns and yields the zero value: never worth failing a run over.
func LookupToolVersions(ctx context.Context, logger *zap.Logger, composer ComposerVersionReporter) report.ToolVersions {
	versions, err := composer.Version(ctx)
	if err != nil {
		logger.Warn("could not determine the composer version", zap.Error(err))
		return report.ToolVersions{}
	}

	logger.Info("tool versions",
		zap.String("composer", versions.Composer),
		zap.String("php", versions.PHP),
	)

	return report.ToolVersions{ComposerVersion: versions.Composer, PHPVersion: versions.PHP}
}
