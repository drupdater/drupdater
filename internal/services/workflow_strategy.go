package services

import (
	"context"

	"github.com/drupdater/drupdater/internal"
)

// WorkflowStrategy defines the interface for different update workflows
type WorkflowStrategy interface {
	// PreUpdate performs actions before the dependency update
	PreUpdate(ctx context.Context, path string) ([]string, bool, error)

	// PostUpdate performs actions after the dependency update
	PostUpdate(ctx context.Context, path string, worktree internal.Worktree) error

	// GenerateBranchName creates a unique branch name for the updates
	GenerateBranchName(path string) string

	// GeneratePRDetails generates PR title and template name for description
	GeneratePRDetails() (string, string)

	// GetTemplateData prepares data for the PR description template
	GetTemplateData(result WorkflowUpdateResult, updateHooks UpdateHooksPerSite) (TemplateData, error)

	// ShouldContinue determines if the workflow should proceed based on pre-update checks
	ShouldContinue(packagesToUpdate []string) bool
}
