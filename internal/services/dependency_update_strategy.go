package services

import (
	"context"
	"fmt"
	"time"

	"github.com/drupdater/drupdater/internal"
	"go.uber.org/zap"
)

// DependencyUpdateStrategy implements the dependency update workflow
type DependencyUpdateStrategy struct {
	logger      *zap.Logger
	config      internal.Config
	current     time.Time
	afterUpdate []AfterUpdate
}

func NewDependencyUpdateStrategy(
	afterUpdate []AfterUpdate,
	logger *zap.Logger,
	config internal.Config,
) *DependencyUpdateStrategy {
	return &DependencyUpdateStrategy{
		afterUpdate: afterUpdate,
		logger:      logger,
		config:      config,
		current:     time.Now(),
	}
}

func (s *DependencyUpdateStrategy) PreUpdate(ctx context.Context, path string) ([]string, bool, error) {
	// For regular dependency updates, we update all packages and don't use minimal changes
	return []string{}, false, nil
}

func (s *DependencyUpdateStrategy) ShouldContinue(packagesToUpdate []string) bool {
	// Dependency updates always continue regardless of packages to update
	return true
}

func (s *DependencyUpdateStrategy) PostUpdate(ctx context.Context, path string, worktree internal.Worktree, result WorkflowUpdateResult) error {
	// Execute all after update hooks
	for _, au := range s.afterUpdate {
		if err := au.Execute(ctx, path, worktree); err != nil {
			s.logger.Error("failed to execute after update", zap.Error(err))
			return err
		}
	}

	return nil
}

func (s *DependencyUpdateStrategy) GenerateBranchName(composerLockHash string) string {
	return fmt.Sprintf("update-%s", s.current.Format("20060102150405"))
}

func (s *DependencyUpdateStrategy) GeneratePRDetails() (string, string) {
	title := fmt.Sprintf("%s: Drupal Maintenance Updates", s.current.Format("January 2006"))
	templateName := "dependency_update.go.tmpl"
	return title, templateName
}

func (s *DependencyUpdateStrategy) GetTemplateData(result WorkflowUpdateResult, updateHooks UpdateHooksPerSite) (TemplateData, error) {
	return TemplateData{
		ComposerDiff:           result.table,
		DependencyUpdateReport: result.updateReport,
		UpdateHooks:            updateHooks,
	}, nil
}
