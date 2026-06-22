package codehosting

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Bitbucket implements the Platform interface for Bitbucket Cloud repositories.
type Bitbucket struct {
	httpClient *http.Client
	apiBaseURL string
	workspace  string
	repoSlug   string
}

// bitbucketTransport adds Bearer token authentication to every request.
type bitbucketTransport struct {
	token     string
	transport http.RoundTripper
}

func (t *bitbucketTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+t.token)
	tr := t.transport
	if tr == nil {
		tr = http.DefaultTransport
	}
	return tr.RoundTrip(req)
}

func newBitbucket(repositoryURL string, token string) *Bitbucket {
	u, err := url.Parse(repositoryURL)
	if err != nil {
		return &Bitbucket{}
	}

	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 {
		return &Bitbucket{}
	}

	return &Bitbucket{
		httpClient: &http.Client{
			Transport: &bitbucketTransport{token: token},
		},
		apiBaseURL: "https://api.bitbucket.org/2.0",
		workspace:  parts[0],
		repoSlug:   strings.TrimSuffix(parts[1], ".git"),
	}
}

func (b *Bitbucket) doRequest(ctx context.Context, method, endpoint string, body interface{}) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, bodyReader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	return b.httpClient.Do(req)
}

type bitbucketBranchName struct {
	Name string `json:"name"`
}

type bitbucketBranchRef struct {
	Branch bitbucketBranchName `json:"branch"`
}

type bitbucketPRRequest struct {
	Title       string             `json:"title"`
	Description string             `json:"description"`
	Source      bitbucketBranchRef `json:"source"`
	Destination bitbucketBranchRef `json:"destination"`
}

type bitbucketPRResponse struct {
	ID    int64 `json:"id"`
	Links struct {
		HTML struct {
			Href string `json:"href"`
		} `json:"html"`
	} `json:"links"`
}

type bitbucketUserResponse struct {
	DisplayName string `json:"display_name"`
}

type bitbucketEmailsResponse struct {
	Values []struct {
		Email     string `json:"email"`
		IsPrimary bool   `json:"is_primary"`
	} `json:"values"`
}

// CreateMergeRequest creates a pull request on Bitbucket.
func (b *Bitbucket) CreateMergeRequest(ctx context.Context, title string, description string, sourceBranch string, targetBranch string) (MergeRequest, error) {
	reqBody := bitbucketPRRequest{
		Title:       title,
		Description: description,
		Source:      bitbucketBranchRef{Branch: bitbucketBranchName{Name: sourceBranch}},
		Destination: bitbucketBranchRef{Branch: bitbucketBranchName{Name: targetBranch}},
	}

	endpoint := fmt.Sprintf("%s/repositories/%s/%s/pullrequests", b.apiBaseURL, b.workspace, b.repoSlug)
	resp, err := b.doRequest(ctx, http.MethodPost, endpoint, reqBody)
	if err != nil {
		return MergeRequest{}, fmt.Errorf("failed to create pull request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return MergeRequest{}, fmt.Errorf("failed to create pull request: %s", string(body))
	}

	var pr bitbucketPRResponse
	if err := json.NewDecoder(resp.Body).Decode(&pr); err != nil {
		return MergeRequest{}, fmt.Errorf("failed to decode pull request response: %w", err)
	}

	return MergeRequest{
		ID:  pr.ID,
		URL: pr.Links.HTML.Href,
	}, nil
}

// DeleteBranch removes a remote branch via the Bitbucket Refs API.
func (b *Bitbucket) DeleteBranch(ctx context.Context, branch string) error {
	endpoint := fmt.Sprintf("%s/repositories/%s/%s/refs/branches/%s", b.apiBaseURL, b.workspace, b.repoSlug, branch)
	resp, err := b.doRequest(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return fmt.Errorf("failed to delete branch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to delete branch: %s", string(body))
	}

	return nil
}

// GetUser returns the display name and primary email of the authenticated user.
func (b *Bitbucket) GetUser(ctx context.Context) (name string, email string) {
	resp, err := b.doRequest(ctx, http.MethodGet, b.apiBaseURL+"/user", nil)
	if err != nil {
		return "", ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", ""
	}

	var user bitbucketUserResponse
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return "", ""
	}
	name = user.DisplayName

	emailResp, err := b.doRequest(ctx, http.MethodGet, b.apiBaseURL+"/user/emails", nil)
	if err != nil {
		return name, ""
	}
	defer emailResp.Body.Close()

	if emailResp.StatusCode != http.StatusOK {
		return name, ""
	}

	var emails bitbucketEmailsResponse
	if err := json.NewDecoder(emailResp.Body).Decode(&emails); err != nil {
		return name, ""
	}

	for _, e := range emails.Values {
		if e.IsPrimary {
			return name, e.Email
		}
	}
	if len(emails.Values) > 0 {
		return name, emails.Values[0].Email
	}

	return name, ""
}
