package codehosting

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/go-github/v68/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestGithub_GetOwner(t *testing.T) {
	gh, err := newGithub("owner/repo", "dummy-token", zap.NewNop())
	require.NoError(t, err)

	assert.Equal(t, "owner", gh.owner)
}

func TestGithub_GetRepo(t *testing.T) {
	gh, err := newGithub("owner/repo", "dummy-token", zap.NewNop())
	require.NoError(t, err)

	assert.Equal(t, "repo", gh.repo)
}

func TestNewGithub_InvalidPath(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{name: "no separator", path: "owner"},
		{name: "empty", path: ""},
		{name: "only separators", path: "//"},
		{name: "missing repo", path: "owner/"},
		{name: "missing owner", path: "/repo"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := newGithub(tt.path, "dummy-token", zap.NewNop())
			require.Error(t, err)
		})
	}
}

func TestNewGithub_WiresUpTheClient(t *testing.T) {
	logger := zap.NewNop()

	gh, err := newGithub("owner/repo", "dummy-token", logger)
	require.NoError(t, err)

	// The surrounding API tests build a Github literal directly, so nothing else asserts what
	// the constructor actually populates.
	assert.NotNil(t, gh.client)
	assert.Equal(t, "owner", gh.owner)
	assert.Equal(t, "repo", gh.repo)
	assert.Same(t, logger, gh.logger)
}

func TestGithub_CreateMergeRequest(t *testing.T) {
	var sent struct {
		Head  string `json:"head"`
		Base  string `json:"base"`
		Title string `json:"title"`
		Body  string `json:"body"`
	}

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		var jsonString []byte
		if r.URL.Path == "/api/v3/repos/test_owner/test_project/pulls" {
			// Check what was sent, not just what came back: without this any of the pull
			// request fields could go missing unnoticed.
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&sent))
			jsonString = []byte(`{"number": 1, "html_url": "http://example.com"}`)
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}

		_, err := w.Write(jsonString)
		assert.NoError(t, err)
	}))
	defer mockServer.Close()

	client, _ := github.NewClient(nil).WithEnterpriseURLs(mockServer.URL, "")

	gh := &Github{
		client: client,
		owner:  "test_owner",
		repo:   "test_project",
	}

	mr, err := gh.CreateMergeRequest(context.Background(), "Test MR", "This is a test MR", "source-branch", "target-branch")

	require.NoError(t, err)
	assert.Equal(t, int64(1), mr.ID)
	assert.Equal(t, "http://example.com", mr.URL)

	assert.Equal(t, "source-branch", sent.Head)
	assert.Equal(t, "target-branch", sent.Base)
	assert.Equal(t, "Test MR", sent.Title)
	assert.Equal(t, "This is a test MR", sent.Body)
}

func TestGithub_GetUser_Returns403FallbackSilently(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"Resource not accessible by integration","errors":[]}`))
	}))
	defer mockServer.Close()

	client, _ := github.NewClient(nil).WithEnterpriseURLs(mockServer.URL, "")
	gh := &Github{client: client, owner: "o", repo: "r"}

	name, email := gh.GetUser(context.Background())

	assert.Equal(t, "github-actions[bot]", name)
	assert.Equal(t, "41898282+github-actions[bot]@users.noreply.github.com", email)
}

func TestGithub_GetUser_Returns403ErrorForBadCredentials(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"Bad credentials","documentation_url":"https://docs.github.com/rest"}`))
	}))
	defer mockServer.Close()

	client, _ := github.NewClient(nil).WithEnterpriseURLs(mockServer.URL, "")
	gh := &Github{client: client, owner: "o", repo: "r"}

	name, email := gh.GetUser(context.Background())

	assert.Empty(t, name)
	assert.Empty(t, email)
}

func TestGithub_GetUser_ReturnsUserOnSuccess(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"login":"octocat","name":"The Octocat","email":"octocat@github.com"}`))
	}))
	defer mockServer.Close()

	client, _ := github.NewClient(nil).WithEnterpriseURLs(mockServer.URL, "")
	gh := &Github{client: client, owner: "o", repo: "r"}

	name, email := gh.GetUser(context.Background())

	assert.Equal(t, "The Octocat", name)
	assert.Equal(t, "octocat@github.com", email)
}

func TestGithub_GetUser_NoEmailFallsBackToNoreply(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":1234567,"login":"octocat","name":"The Octocat","email":""}`))
	}))
	defer mockServer.Close()

	client, _ := github.NewClient(nil).WithEnterpriseURLs(mockServer.URL, "")
	gh := &Github{client: client, owner: "o", repo: "r"}

	name, email := gh.GetUser(context.Background())

	assert.Equal(t, "The Octocat", name)
	assert.Equal(t, "1234567+octocat@users.noreply.github.com", email)
}

func TestGithub_CreateMergeRequest_HonorsContext(t *testing.T) {
	// A cancelled context must abort before any request is sent. This would have
	// passed silently when the implementation used context.TODO().
	client, _ := github.NewClient(nil).WithEnterpriseURLs("http://example.invalid", "")
	gh := &Github{client: client, owner: "o", repo: "r"}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := gh.CreateMergeRequest(ctx, "Test MR", "body", "source", "target")
	require.ErrorIs(t, err, context.Canceled)
}

func TestGithub_GetUser_HonorsContext(t *testing.T) {
	client, _ := github.NewClient(nil).WithEnterpriseURLs("http://example.invalid", "")
	gh := &Github{client: client, owner: "o", repo: "r"}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// A request error (not the Actions 403 fallback) yields empty strings.
	name, email := gh.GetUser(ctx)
	assert.Empty(t, name)
	assert.Empty(t, email)
}

func TestGithub_DeleteBranch_Success(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && r.URL.Path == "/api/v3/repos/test_owner/test_project/git/refs/heads/update-abc123" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer mockServer.Close()

	client, _ := github.NewClient(nil).WithEnterpriseURLs(mockServer.URL, "")
	gh := &Github{client: client, owner: "test_owner", repo: "test_project"}

	err := gh.DeleteBranch(context.Background(), "update-abc123")
	require.NoError(t, err)
}

func TestGithub_DeleteBranch_Error(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"message":"Reference does not exist"}`))
	}))
	defer mockServer.Close()

	client, _ := github.NewClient(nil).WithEnterpriseURLs(mockServer.URL, "")
	gh := &Github{client: client, owner: "o", repo: "r"}

	err := gh.DeleteBranch(context.Background(), "nonexistent-branch")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to delete branch")
}

func TestGithub_DeleteBranch_HonorsContext(t *testing.T) {
	client, _ := github.NewClient(nil).WithEnterpriseURLs("http://example.invalid", "")
	gh := &Github{client: client, owner: "o", repo: "r"}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := gh.DeleteBranch(ctx, "some-branch")
	require.ErrorIs(t, err, context.Canceled)
}

// newAutoMergePRServer serves the PR endpoint and a successful GraphQL mutation, capturing the
// mergeMethod that was sent. WithEnterpriseURLs puts "graphql" under /api/v3/, unlike real GHES.
func newAutoMergePRServer(t *testing.T, baseRepoJSON string, gotMethod *string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/repos/owner/repo/pulls/1":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"number":1,"node_id":"PR_kwDOABCDEF123","base":{"repo":` + baseRepoJSON + `}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v3/graphql":
			var body struct {
				Variables struct {
					MergeMethod string `json:"mergeMethod"`
				} `json:"variables"`
			}
			// A decode failure leaves gotMethod empty, which fails the caller's assertion;
			// testifylint forbids require inside an HTTP handler.
			_ = json.NewDecoder(r.Body).Decode(&body)
			*gotMethod = body.Variables.MergeMethod
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":{"enablePullRequestAutoMerge":{"pullRequest":{"autoMergeRequest":{"mergeMethod":"MERGE"}}}}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func TestGithub_EnableAutoMerge_Success(t *testing.T) {
	var gotMethod string
	mockServer := newAutoMergePRServer(t, `{"allow_merge_commit":true}`, &gotMethod)

	client, _ := github.NewClient(nil).WithEnterpriseURLs(mockServer.URL, "")
	gh := &Github{client: client, owner: "owner", repo: "repo"}

	err := gh.EnableAutoMerge(context.Background(), MergeRequest{ID: 1})
	require.NoError(t, err)
	assert.Equal(t, "MERGE", gotMethod)
}

// TestGithub_EnableAutoMerge_UsesPermittedMergeMethod covers repositories that disable merge
// commits: requesting a method the repository forbids makes the mutation fail outright.
func TestGithub_EnableAutoMerge_UsesPermittedMergeMethod(t *testing.T) {
	tests := map[string]struct {
		baseRepo string
		want     string
	}{
		"squash only":     {`{"allow_merge_commit":false,"allow_squash_merge":true}`, "SQUASH"},
		"rebase only":     {`{"allow_merge_commit":false,"allow_rebase_merge":true}`, "REBASE"},
		"merge preferred": {`{"allow_merge_commit":true,"allow_squash_merge":true}`, "MERGE"},
		"flags absent":    {`{}`, "MERGE"},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var gotMethod string
			mockServer := newAutoMergePRServer(t, tt.baseRepo, &gotMethod)

			client, _ := github.NewClient(nil).WithEnterpriseURLs(mockServer.URL, "")
			gh := &Github{client: client, owner: "owner", repo: "repo"}

			require.NoError(t, gh.EnableAutoMerge(context.Background(), MergeRequest{ID: 1}))
			assert.Equal(t, tt.want, gotMethod)
		})
	}
}

func TestGithub_EnableAutoMerge_GraphQLError(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/repos/owner/repo/pulls/1":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"number":1,"node_id":"PR_kwDOABCDEF123"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v3/graphql":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"errors":[{"message":"Auto-merge is not allowed for this repository"},{"message":"second problem"}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mockServer.Close()

	client, _ := github.NewClient(nil).WithEnterpriseURLs(mockServer.URL, "")
	gh := &Github{client: client, owner: "owner", repo: "repo"}

	err := gh.EnableAutoMerge(context.Background(), MergeRequest{ID: 1})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Auto-merge is not allowed for this repository")
	assert.Contains(t, err.Error(), "second problem", "every GraphQL error should be surfaced")
}

func TestGithub_EnableAutoMerge_GetPRError(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"Not Found"}`))
	}))
	defer mockServer.Close()

	client, _ := github.NewClient(nil).WithEnterpriseURLs(mockServer.URL, "")
	gh := &Github{client: client, owner: "owner", repo: "repo"}

	err := gh.EnableAutoMerge(context.Background(), MergeRequest{ID: 1})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "could not enable auto merge for PR 1")
}

func TestGithub_EnableAutoMerge_HonorsContext(t *testing.T) {
	client, _ := github.NewClient(nil).WithEnterpriseURLs("http://example.invalid", "")
	gh := &Github{client: client, owner: "o", repo: "r"}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := gh.EnableAutoMerge(ctx, MergeRequest{ID: 1})
	require.ErrorIs(t, err, context.Canceled)
}

func TestIsGitHubActionsToken403_ReturnsFalseForNonGitHubError(t *testing.T) {
	result := isGitHubActionsToken403(nil, errors.New("plain network error"))
	assert.False(t, result)
}

func TestIsGitHubActionsToken403_ReturnsTrueWhenRespFallback(t *testing.T) {
	// github.ErrorResponse with nil Response but matching integration message —
	// status code comes from the resp fallback parameter.
	ghErr := &github.ErrorResponse{
		Message: "Resource not accessible by integration",
	}
	resp := &github.Response{Response: &http.Response{StatusCode: http.StatusForbidden}}
	assert.True(t, isGitHubActionsToken403(resp, ghErr))
}
