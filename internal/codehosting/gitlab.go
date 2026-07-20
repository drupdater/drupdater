package codehosting

import (
	"context"
	"fmt"

	gitlab "gitlab.com/gitlab-org/api/client-go"
	"go.uber.org/zap"
)

// Gitlab implements the Platform interface for GitLab repositories.
type Gitlab struct {
	client      *gitlab.Client
	projectPath string
	logger      *zap.Logger
}

// newGitlab creates a new GitLab client for the given host and "group/project" path.
func newGitlab(host string, path string, token string, logger *zap.Logger) (*Gitlab, error) {
	if host == "" {
		return nil, fmt.Errorf("could not determine GitLab host from repository URL")
	}

	gitlabClient, err := gitlab.NewClient(token, gitlab.WithBaseURL("https://"+host))
	if err != nil {
		return nil, fmt.Errorf("failed to create GitLab client: %w", err)
	}

	return &Gitlab{
		client:      gitlabClient,
		projectPath: path,
		logger:      logger,
	}, nil
}

// CreateMergeRequest creates a merge request on GitLab.
func (g *Gitlab) CreateMergeRequest(ctx context.Context, title string, description string, sourceBranch string, targetBranch string) (MergeRequest, error) {
	mr, _, err := g.client.MergeRequests.CreateMergeRequest(g.projectPath, &gitlab.CreateMergeRequestOptions{
		SourceBranch: &sourceBranch,
		TargetBranch: &targetBranch,
		Title:        &title,
		Description:  &description,
	}, gitlab.WithContext(ctx))

	if err != nil {
		return MergeRequest{}, fmt.Errorf("failed to create merge request: %w", err)
	}

	return MergeRequest{
		ID:  mr.IID,
		URL: mr.WebURL,
	}, nil
}

// DeleteBranch removes a remote branch via the GitLab Branches API.
func (g *Gitlab) DeleteBranch(ctx context.Context, branch string) error {
	_, err := g.client.Branches.DeleteBranch(g.projectPath, branch, gitlab.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("failed to delete branch: %w", err)
	}
	return nil
}

func (g *Gitlab) GetUser(ctx context.Context) (name string, email string) {
	user, _, err := g.client.Users.CurrentUser(gitlab.WithContext(ctx))
	if err != nil {
		if g.logger != nil {
			g.logger.Error("failed to get GitLab user", zap.Error(err))
		}
		return "", ""
	}

	return user.Name, user.Email
}
