package codehosting

import (
	"context"
	"strings"
)

// Platform defines the interface for version control system providers.
type Platform interface {
	// CreateMergeRequest creates a merge/pull request on the platform.
	CreateMergeRequest(ctx context.Context, title string, description string, sourceBranch string, targetBranch string) (MergeRequest, error)

	// DeleteBranch removes a remote branch from the platform.
	DeleteBranch(ctx context.Context, branch string) error

	// GetUser returns the user name and email from the platform.
	GetUser(ctx context.Context) (name string, email string)
}

// MergeRequest represents a merge or pull request on a code hosting platform.
type MergeRequest struct {
	ID  int64  `json:"id"`
	URL string `json:"url"`
}

// DefaultVcsProviderFactory creates appropriate Platform implementations
// based on repository URLs.
type DefaultVcsProviderFactory struct{}

// NewDefaultVcsProviderFactory creates a new factory for VCS providers.
func NewDefaultVcsProviderFactory() *DefaultVcsProviderFactory {
	return &DefaultVcsProviderFactory{}
}

// Create returns a Platform implementation appropriate for the given repository URL.
func (vpf *DefaultVcsProviderFactory) Create(repositoryURL string, token string) Platform {
	switch vpf.getProvider(repositoryURL) {
	case "github":
		return newGithub(repositoryURL, token)
	case "bitbucket":
		return newBitbucket(repositoryURL, token)
	default:
		return newGitlab(repositoryURL, token)
	}
}

// getProvider determines the VCS provider type from a repository URL.
func (vpf *DefaultVcsProviderFactory) getProvider(repositoryURL string) string {
	if strings.Contains(repositoryURL, "gitlab") {
		return "gitlab"
	}
	if strings.Contains(repositoryURL, "github") {
		return "github"
	}
	if strings.Contains(repositoryURL, "bitbucket") {
		return "bitbucket"
	}
	return ""
}
