package codehosting

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/google/go-github/v68/github"
	"github.com/spf13/afero"
)

type Github struct {
	client *github.Client
	owner  string
	repo   string
	fs     afero.Fs
}

func newGithub(repositoryURL string, token string) *Github {

	u, _ := url.Parse(repositoryURL)

	return &Github{
		client: github.NewClient(nil).WithAuthToken(token),
		owner:  strings.Split(u.Path, "/")[1],
		repo:   strings.Split(u.Path, "/")[2],
		fs:     afero.NewOsFs(),
	}
}

func (g Github) CreateMergeRequest(title string, description string, sourceBranch string, targetBranch string) (MergeRequest, error) {
	mr, _, err := g.client.PullRequests.Create(context.TODO(), g.owner, g.repo, &github.NewPullRequest{
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

// logError logs an error. This is a placeholder for proper error handling.
func (g *Github) logError(err error) {
	// In a real implementation, this would use a proper logging mechanism
	fmt.Printf("Error: %v\n", err)
}

// GetUser returns the name and email of the authenticated user.
// When called with a GITHUB_TOKEN (Actions bot), GET /user returns 403 with the
// message "Resource not accessible by integration"; in that case we fall back to
// the well-known github-actions[bot] identity so that git commits are attributed
// correctly without requiring a PAT.
// Any other error (including a 403 from bad user credentials) is logged and
// returns empty strings so callers can detect the failure.
func (g *Github) GetUser() (name string, email string) {
	user, resp, err := g.client.Users.Get(context.Background(), "")
	if err != nil {
		if isGitHubActionsToken403(resp, err) {
			return "github-actions[bot]", "41898282+github-actions[bot]@users.noreply.github.com"
		}
		g.logError(fmt.Errorf("failed to get user: %w", err))
		return "", ""
	}

	email = user.GetEmail()
	if email == "" {
		email = fmt.Sprintf("%d+%s@users.noreply.github.com", user.GetID(), user.GetLogin())
	}
	return user.GetName(), email
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
