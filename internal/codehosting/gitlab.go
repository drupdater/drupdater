package codehosting

import (
	"context"
	"fmt"
	"net/http"
	"time"

	gitlab "gitlab.com/gitlab-org/api/client-go"
	"go.uber.org/zap"
)

// Gitlab implements the Platform interface for GitLab repositories.
type Gitlab struct {
	client        *gitlab.Client
	projectPath   string
	logger        *zap.Logger
	retryInterval time.Duration
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
		client:        gitlabClient,
		projectPath:   path,
		logger:        logger,
		retryInterval: 5 * time.Second,
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

// EnableAutoMerge sets the MR to merge automatically when the pipeline succeeds.
// It first polls until the MR becomes mergeable (up to 7 attempts), then accepts
// with auto_merge. The accept call is retried up to 4 times on HTTP 405, which
// GitLab can return transiently even when the merge_status is can_be_merged.
func (g *Gitlab) EnableAutoMerge(ctx context.Context, mr MergeRequest) error {
	if err := g.waitForMergeable(ctx, mr.ID); err != nil {
		return err
	}
	return g.acceptWithAutoMerge(ctx, mr.ID)
}

func (g *Gitlab) waitForMergeable(ctx context.Context, iid int64) error {
	for i := 0; i < 7; i++ {
		if i > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(g.retryInterval):
			}
		}
		details, _, err := g.client.MergeRequests.GetMergeRequest(g.projectPath, iid, nil, gitlab.WithContext(ctx))
		if err != nil {
			return fmt.Errorf("could not set auto merge for MR %d: %w", iid, err)
		}
		if details.DetailedMergeStatus == "mergeable" {
			return nil
		}
	}
	return fmt.Errorf("could not set auto merge for MR %d: merge request did not become mergeable after retries", iid)
}

func (g *Gitlab) acceptWithAutoMerge(ctx context.Context, iid int64) error {
	autoMerge := true
	removeSourceBranch := true
	opts := &gitlab.AcceptMergeRequestOptions{
		AutoMerge:                &autoMerge,
		ShouldRemoveSourceBranch: &removeSourceBranch,
	}

	var lastErr error
	for i := 0; i < 4; i++ {
		if i > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(g.retryInterval):
			}
		}
		_, resp, err := g.client.MergeRequests.AcceptMergeRequest(g.projectPath, iid, opts, gitlab.WithContext(ctx))
		if err == nil {
			return nil
		}
		lastErr = err
		if resp == nil || resp.StatusCode != http.StatusMethodNotAllowed {
			break
		}
	}
	return fmt.Errorf("could not set auto merge for MR %d: %w", iid, lastErr)
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
