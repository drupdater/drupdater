package codehosting

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	gitlab "gitlab.com/gitlab-org/api/client-go"
)

// Gitlab implements the Platform interface for GitLab repositories.
type Gitlab struct {
	client      *gitlab.Client
	projectPath string
}

// newGitlab creates a new GitLab client based on repository URL and token.
func newGitlab(repositoryURL string, token string) *Gitlab {
	u, err := url.Parse(repositoryURL)
	if err != nil {
		return &Gitlab{}
	}

	baseURL := fmt.Sprintf("%s://%s", u.Scheme, u.Host)

	gitlabClient, err := gitlab.NewClient(token, gitlab.WithBaseURL(baseURL))
	if err != nil {
		fmt.Printf("Error creating GitLab client: %v\n", err)
		return &Gitlab{}
	}

	return &Gitlab{
		client:      gitlabClient,
		projectPath: strings.TrimSuffix(strings.Trim(u.Path, "/"), ".git"),
	}
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
		fmt.Printf("Error getting GitLab user: %v\n", err)
		return "", ""
	}

	return user.Name, user.Email
}
