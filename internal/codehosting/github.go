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

// newGithub builds a GitHub platform from an "owner/repo" path, erroring rather than panicking
// when a segment is missing.
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

func (g *Github) CreateMergeRequest(ctx context.Context, title string, description string, sourceBranch string, targetBranch string) (MergeRequest, error) {
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

// GetUser returns the authenticated user's name and email.
//
// A GITHUB_TOKEN (Actions bot) gets 403 "Resource not accessible by integration" from GET /user;
// that case falls back to the github-actions[bot] identity, so commits are attributed correctly
// without a PAT. Any other error yields empty strings so callers can detect the failure.
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

// EnableAutoMerge merges the PR once every required status check passes.
//
// The endpoint: go-github's base URL is the REST root, so "graphql" resolves correctly for
// github.com. Enterprise serves GraphQL at /api/graphql, outside the /api/v3/ prefix, so this
// path needs adjusting if newGithub ever gains enterprise support.
func (g *Github) EnableAutoMerge(ctx context.Context, mr MergeRequest) error {
	pr, _, err := g.client.PullRequests.Get(ctx, g.owner, g.repo, int(mr.ID))
	if err != nil {
		return fmt.Errorf("could not enable auto merge for PR %d: %w", mr.ID, err)
	}

	body := struct {
		Query     string         `json:"query"`
		Variables map[string]any `json:"variables"`
	}{
		Query: `mutation($prId: ID!, $mergeMethod: PullRequestMergeMethod!) {
			enablePullRequestAutoMerge(input: {pullRequestId: $prId, mergeMethod: $mergeMethod}) {
				pullRequest { autoMergeRequest { mergeMethod } }
			}
		}`,
		Variables: map[string]any{
			"prId":        pr.GetNodeID(),
			"mergeMethod": mergeMethodFor(pr.GetBase().GetRepo()),
		},
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
		messages := make([]string, 0, len(result.Errors))
		for _, e := range result.Errors {
			messages = append(messages, e.Message)
		}
		return fmt.Errorf("could not enable auto merge for PR %d: %s", mr.ID, strings.Join(messages, "; "))
	}
	return nil
}

// mergeMethodFor picks a method the repository permits: requesting a disallowed one fails the
// mutation, and many repositories allow only squash or only rebase. With no flags set at all it
// falls back to MERGE, whose rejection at least names the problem.
//
// Unlike GitLab there is no per-request "delete source branch" option; that is the repository's
// delete_branch_on_merge setting.
func mergeMethodFor(repo *github.Repository) string {
	switch {
	case repo.GetAllowMergeCommit():
		return "MERGE"
	case repo.GetAllowSquashMerge():
		return "SQUASH"
	case repo.GetAllowRebaseMerge():
		return "REBASE"
	default:
		return "MERGE"
	}
}

// isGitHubActionsToken403 matches only the 403 an Actions token gets from /user, identified by
// its "Resource not accessible by integration" message. Bad credentials and an under-scoped PAT
// also give 403, with a different message, and must not be suppressed.
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
