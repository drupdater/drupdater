package codehosting

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/go-github/v68/github"
	"go.uber.org/zap"
)

type Github struct {
	client *github.Client
	owner  string
	repo   string
	logger *zap.Logger
}

// newGithub builds a GitHub platform from an "owner/repo" path. It returns an error rather
// than panicking when the path does not carry both segments.
func newGithub(path string, token string, logger *zap.Logger) (*Github, error) {
	owner, repo, found := strings.Cut(strings.Trim(path, "/"), "/")
	if !found || owner == "" || repo == "" {
		return nil, fmt.Errorf("could not determine owner and repository from %q", path)
	}

	return &Github{
		client: github.NewClient(nil).WithAuthToken(token),
		owner:  owner,
		repo:   repo,
		logger: logger,
	}, nil
}

func (g Github) CreateMergeRequest(ctx context.Context, title string, description string, sourceBranch string, targetBranch string) (MergeRequest, error) {
	mr, _, err := g.client.PullRequests.Create(ctx, g.owner, g.repo, &github.NewPullRequest{
		Head:  &sourceBranch,
		Base:  &targetBranch,
		Title: &title,
		Body:  &description,
	})

	if err != nil {
		return MergeRequest{}, fmt.Errorf("failed to create pull request: %w", err)
	}
	return MergeRequest{
		ID:  int64(mr.GetNumber()),
		URL: mr.GetHTMLURL(),
	}, nil
}

// DeleteBranch removes a remote branch via the GitHub Git refs API.
func (g *Github) DeleteBranch(ctx context.Context, branch string) error {
	_, err := g.client.Git.DeleteRef(ctx, g.owner, g.repo, "refs/heads/"+branch)
	if err != nil {
		return fmt.Errorf("failed to delete branch: %w", err)
	}
	return nil
}

// GetUser returns the name and email of the authenticated user.
// When called with a GITHUB_TOKEN (Actions bot), GET /user returns 403 with the
// message "Resource not accessible by integration"; in that case we fall back to
// the well-known github-actions[bot] identity so that git commits are attributed
// correctly without requiring a PAT.
// Any other error (including a 403 from bad user credentials) is logged and
// returns empty strings so callers can detect the failure.
func (g *Github) GetUser(ctx context.Context) (name string, email string) {
	user, resp, err := g.client.Users.Get(ctx, "")
	if err != nil {
		if isGitHubActionsToken403(resp, err) {
			return "github-actions[bot]", "41898282+github-actions[bot]@users.noreply.github.com"
		}
		if g.logger != nil {
			g.logger.Error("failed to get user", zap.Error(err))
		}
		return "", ""
	}

	email = user.GetEmail()
	if email == "" {
		email = fmt.Sprintf("%d+%s@users.noreply.github.com", user.GetID(), user.GetLogin())
	}
	return user.GetName(), email
}

// EnableAutoMerge enables auto-merge on a GitHub pull request via the GraphQL API.
// When all required status checks pass the PR is merged automatically.
func (g *Github) EnableAutoMerge(ctx context.Context, mr MergeRequest) error {
	pr, _, err := g.client.PullRequests.Get(ctx, g.owner, g.repo, int(mr.ID))
	if err != nil {
		return fmt.Errorf("could not enable auto merge for PR %d: %w", mr.ID, err)
	}

	body := struct {
		Query     string         `json:"query"`
		Variables map[string]any `json:"variables"`
	}{
		Query: `mutation($prId: ID!) {
			enablePullRequestAutoMerge(input: {pullRequestId: $prId, mergeMethod: MERGE}) {
				pullRequest { autoMergeRequest { mergeMethod } }
			}
		}`,
		Variables: map[string]any{"prId": pr.GetNodeID()},
	}

	req, err := g.client.NewRequest("POST", "graphql", body)
	if err != nil {
		return fmt.Errorf("could not enable auto merge for PR %d: %w", mr.ID, err)
	}

	var result struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if _, err = g.client.Do(ctx, req, &result); err != nil {
		return fmt.Errorf("could not enable auto merge for PR %d: %w", mr.ID, err)
	}
	if len(result.Errors) > 0 {
		return fmt.Errorf("could not enable auto merge for PR %d: %s", mr.ID, result.Errors[0].Message)
	}
	return nil
}

// isGitHubActionsToken403 reports whether the error is the specific 403 returned
// by GitHub for an Actions integration token (GITHUB_TOKEN) that cannot access
// the /user endpoint. Such responses carry the message
// "Resource not accessible by integration".
//
// A real user whose credentials are wrong or whose PAT lacks the required scope
// also receives a 403, but with a different message; those errors must NOT be
// suppressed so callers can detect and surface them.
func isGitHubActionsToken403(resp *github.Response, err error) bool {
	var ghErr *github.ErrorResponse
	if !errors.As(err, &ghErr) {
		return false
	}
	statusCode := 0
	if ghErr.Response != nil {
		statusCode = ghErr.Response.StatusCode
	} else if resp != nil {
		statusCode = resp.StatusCode
	}
	return statusCode == 403 && strings.Contains(ghErr.Message, "Resource not accessible by integration")
}
