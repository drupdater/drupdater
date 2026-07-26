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

// Attempt budgets for EnableAutoMerge. GitLab computes mergeability asynchronously, so the
// status right after MR creation is usually still pending; the accept call can also lose a
// race with that computation and answer 405. Both waits are bounded so a run can't hang —
// worst case is (maxMergeStatusChecks + maxAcceptAttempts - 2) * retryInterval.
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
// Every other value is a settled answer and is safe to act on — including ci_must_pass and
// ci_still_running, which are precisely the states auto-merge exists for: the MR is not
// mergeable *yet*, and GitLab should merge it once the pipeline goes green. Waiting for
// "mergeable" instead would defeat the feature, because a pipeline-gated MR only reports
// "mergeable" once it could already be merged outright.
var pendingMergeStatuses = map[string]bool{
	"preparing":         true,
	"checking":          true,
	"unchecked":         true,
	"approvals_syncing": true,
}

// EnableAutoMerge sets the MR to merge automatically once its pipeline succeeds. It first
// waits for GitLab to settle the merge status (at most maxMergeStatusChecks polls), then
// accepts with auto_merge. GitLab decides from there whether to queue the merge or perform
// it immediately, so an MR with no pipeline is merged straight away.
func (g *Gitlab) EnableAutoMerge(ctx context.Context, mr MergeRequest) error {
	status, err := g.waitForSettledMergeStatus(ctx, mr.ID)
	if err != nil {
		return err
	}
	return g.acceptWithAutoMerge(ctx, mr.ID, status)
}

// waitForSettledMergeStatus polls the MR until detailed_merge_status is no longer one of the
// pending values, and returns that settled status. It deliberately does not require a
// mergeable status: see pendingMergeStatuses.
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

// acceptWithAutoMerge accepts the MR with auto_merge set. GitLab documents HTTP 405 on this
// endpoint as "the merge request cannot merge" — a draft, a conflict, or missing approvals all
// land there — but it can also appear briefly right after the merge status settles, so the call
// is retried up to maxAcceptAttempts times in total. status is the settled
// detailed_merge_status and is reported on failure, since it usually names the real blocker.
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
