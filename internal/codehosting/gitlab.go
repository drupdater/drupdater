package codehosting

import (
	"context"
	"fmt"
	"net/http"
	"time"

	gitlab "gitlab.com/gitlab-org/api/client-go"
	"go.uber.org/zap"
)

type Gitlab struct {
	client        *gitlab.Client
	projectPath   string
	logger        *zap.Logger
	retryInterval time.Duration
}

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

// Attempt budgets for EnableAutoMerge. GitLab computes mergeability asynchronously, so the
// status right after MR creation is usually pending and the accept call can lose the race and
// answer 405. Bounded so a run can't hang: (maxMergeStatusChecks + maxAcceptAttempts - 2) *
// retryInterval.
const (
	maxMergeStatusChecks = 7
	maxAcceptAttempts    = 4
)

// pendingMergeStatuses are the detailed_merge_status values that mean GitLab has not finished
// working out whether the MR can merge. These are the only four of the documented values whose
// meaning is "still computing" (see the merge status section of the merge requests API docs):
//
//	preparing          merge request diff is being created
//	checking           Git is testing if a valid merge is possible
//	unchecked          Git has not yet tested if a valid merge is possible
//	approvals_syncing  the merge request's approvals are syncing
//
// Every other value is settled and safe to act on, including ci_must_pass and ci_still_running
// — the states auto-merge exists for. Waiting for "mergeable" would defeat the feature: a
// pipeline-gated MR only reports it once it could already be merged outright.
var pendingMergeStatuses = map[string]bool{
	"preparing":         true,
	"checking":          true,
	"unchecked":         true,
	"approvals_syncing": true,
}

// EnableAutoMerge waits for GitLab to settle the merge status, then accepts with auto_merge.
// GitLab decides from there whether to queue the merge, so an MR with no pipeline merges now.
func (g *Gitlab) EnableAutoMerge(ctx context.Context, mr MergeRequest) error {
	status, err := g.waitForSettledMergeStatus(ctx, mr.ID)
	if err != nil {
		return err
	}
	return g.acceptWithAutoMerge(ctx, mr.ID, status)
}

// waitForSettledMergeStatus polls until detailed_merge_status is no longer pending, and returns
// it. Deliberately does not require a mergeable status: see pendingMergeStatuses.
func (g *Gitlab) waitForSettledMergeStatus(ctx context.Context, iid int64) (string, error) {
	for i := 0; i < maxMergeStatusChecks; i++ {
		if i > 0 {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(g.retryInterval):
			}
		}
		details, _, err := g.client.MergeRequests.GetMergeRequest(g.projectPath, iid, nil, gitlab.WithContext(ctx))
		if err != nil {
			return "", fmt.Errorf("could not set auto merge for MR %d: %w", iid, err)
		}
		if !pendingMergeStatuses[details.DetailedMergeStatus] {
			return details.DetailedMergeStatus, nil
		}
	}
	return "", fmt.Errorf("could not set auto merge for MR %d: merge status was still pending after %d checks", iid, maxMergeStatusChecks)
}

// acceptWithAutoMerge accepts the MR with auto_merge set. GitLab's 405 here means "cannot
// merge" — a draft, a conflict, missing approvals — but also appears briefly just after the
// status settles, hence the retries. status is reported on failure: it names the real blocker.
func (g *Gitlab) acceptWithAutoMerge(ctx context.Context, iid int64, status string) error {
	autoMerge := true
	removeSourceBranch := true
	opts := &gitlab.AcceptMergeRequestOptions{
		AutoMerge:                &autoMerge,
		ShouldRemoveSourceBranch: &removeSourceBranch,
	}

	var lastErr error
	for i := 0; i < maxAcceptAttempts; i++ {
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
	return fmt.Errorf("could not set auto merge for MR %d (merge status %q): %w", iid, status, lastErr)
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
