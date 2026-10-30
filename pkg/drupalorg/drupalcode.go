package drupalorg

import (
	"context"
	"fmt"
	"net/http"

	gitlab "gitlab.com/gitlab-org/api/client-go"
)

const drupalCodeAPI = "https://git.drupalcode.org/api/v4"

// DrupalCode is git.drupalcode.org, the GitLab holding every project's history and issue forks.
type DrupalCode struct {
	client *gitlab.Client
}

// ForkMergeRequest is a merge request into a project from an issue's fork.
type ForkMergeRequest struct {
	SHA    string
	WebURL string
}

// NewDrupalCode authenticates with token; drupalcode answers no API call anonymously.
func NewDrupalCode(token string) *DrupalCode {
	d, err := newDrupalCode(token, drupalCodeAPI)
	if err != nil {
		// Only an unparseable base URL fails, and drupalCodeAPI is a constant.
		panic(err)
	}
	return d
}

func newDrupalCode(token string, baseURL string) (*DrupalCode, error) {
	client, err := gitlab.NewClient(token, gitlab.WithBaseURL(baseURL))
	if err != nil {
		return nil, fmt.Errorf("failed to create the drupalcode client: %w", err)
	}
	return &DrupalCode{client: client}, nil
}

// IsFixedIn reports whether a commit on ref names the issue, which is how drupal.org records a fix.
func (d *DrupalCode) IsFixedIn(ctx context.Context, project string, issueID string, ref string) (bool, error) {
	commits, _, err := d.client.Search.CommitsByProject("project/"+project, issueID,
		&gitlab.SearchOptions{Ref: &ref}, gitlab.WithContext(ctx))
	if err != nil {
		return false, fmt.Errorf("failed to search %s for issue %s: %w", project, issueID, err)
	}
	return len(commits) != 0, nil
}

// IssueForkMergeRequests returns the merge requests into project from the issue's fork, newest first.
func (d *DrupalCode) IssueForkMergeRequests(ctx context.Context, project string, issueID string) ([]ForkMergeRequest, error) {
	fork, _, err := d.client.Projects.GetProject("issue/"+project+"-"+issueID, &gitlab.GetProjectOptions{}, gitlab.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("failed to get the fork of issue %s: %w", issueID, err)
	}

	// A raw request: ListProjectMergeRequests cannot filter on source_project_id.
	opt := struct {
		gitlab.ListProjectMergeRequestsOptions
		SourceProjectID int64 `url:"source_project_id"`
	}{SourceProjectID: fork.ID}
	req, err := d.client.NewRequest(http.MethodGet, "projects/project%2F"+project+"/merge_requests", opt,
		[]gitlab.RequestOptionFunc{gitlab.WithContext(ctx)})
	if err != nil {
		return nil, fmt.Errorf("failed to build the merge request query: %w", err)
	}
	var mergeRequests []*gitlab.BasicMergeRequest
	if _, err := d.client.Do(req, &mergeRequests); err != nil {
		return nil, fmt.Errorf("failed to list the merge requests from the fork of issue %s: %w", issueID, err)
	}

	out := make([]ForkMergeRequest, 0, len(mergeRequests))
	for _, mr := range mergeRequests {
		out = append(out, ForkMergeRequest{SHA: mr.SHA, WebURL: mr.WebURL})
	}
	return out, nil
}
