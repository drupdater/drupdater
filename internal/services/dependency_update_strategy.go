package services

import (
	"context"
	"fmt"
	"time"

	"github.com/drupdater/drupdater/internal"
	"github.com/drupdater/drupdater/internal/addon"
	"github.com/gookit/event"
	"go.uber.org/zap"
)

// DependencyUpdateStrategy implements the dependency update workflow
type DependencyUpdateStrategy struct {
	logger  *zap.Logger
	config  internal.Config
	current time.Time
}

func NewDependencyUpdateStrategy(
	logger *zap.Logger,
	config internal.Config,
) *DependencyUpdateStrategy {
	return &DependencyUpdateStrategy{
		logger:  logger,
		config:  config,
		current: time.Now(),
	}
}

func (s *DependencyUpdateStrategy) PreUpdate(_ context.Context, _ string) ([]string, bool, error) {
	// For regular dependency updates, we update all packages and don't use minimal changes
	return []string{}, false, nil
}

func (s *DependencyUpdateStrategy) ShouldContinue(_ []string) bool {
	// Dependency updates always continue regardless of packages to update
	return true
}

func (s *DependencyUpdateStrategy) PostUpdate(ctx context.Context, path string, worktree internal.Worktree) error {
	e := addon.NewPostCodeUpdateEvent(ctx, path, worktree)
	event.AddEvent(e)

	return event.FireEvent(e)
}

func (s *DependencyUpdateStrategy) GenerateBranchName(_ string) string {
	return fmt.Sprintf("update-%s", s.current.Format("20060102150405"))
}

func (s *DependencyUpdateStrategy) GeneratePRDetails() (string, string) {
	title := fmt.Sprintf("%s: Drupal Maintenance Updates", s.current.Format("January 2006"))
	templateName := "dependency_update.go.tmpl"
	return title, templateName
}

func (s *DependencyUpdateStrategy) GetTemplateData(result WorkflowUpdateResult, updateHooks UpdateHooksPerSite) (TemplateData, error) {
	return TemplateData{
		ComposerDiff: result.table,
		UpdateHooks:  updateHooks,
	}, nil
}
