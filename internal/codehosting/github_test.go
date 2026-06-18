package codehosting

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/go-github/v68/github"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
)

func TestGithub_GetOwner(t *testing.T) {
	// Setup
	gh := newGithub("https://github.com/owner/repo", "dummy-token")

	// Assert
	assert.Equal(t, "owner", gh.owner)
}

func TestGithub_GetRepo(t *testing.T) {
	// Setup
	gh := newGithub("https://github.com/owner/repo", "dummy-token")

	// Assert
	assert.Equal(t, "repo", gh.repo)
}

func TestGithub_CreateMergeRequest(t *testing.T) {
	// Setup mock HTTP server
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		var jsonString []byte
		if r.URL.Path == "/api/v3/repos/test_owner/test_project/pulls" {
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
		fs:     afero.NewMemMapFs(),
	}

	// Execute
	mr, err := gh.CreateMergeRequest(context.Background(), "Test MR", "This is a test MR", "source-branch", "target-branch")

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, int64(1), mr.ID)
	assert.Equal(t, "http://example.com", mr.URL)
}

func TestGithub_GetUser_Returns403FallbackSilently(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"Resource not accessible by integration","errors":[]}`))
	}))
	defer mockServer.Close()

	client, _ := github.NewClient(nil).WithEnterpriseURLs(mockServer.URL, "")
	gh := &Github{client: client, owner: "o", repo: "r", fs: afero.NewMemMapFs()}

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
	gh := &Github{client: client, owner: "o", repo: "r", fs: afero.NewMemMapFs()}

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
	gh := &Github{client: client, owner: "o", repo: "r", fs: afero.NewMemMapFs()}

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
	gh := &Github{client: client, owner: "o", repo: "r", fs: afero.NewMemMapFs()}

	name, email := gh.GetUser(context.Background())

	assert.Equal(t, "The Octocat", name)
	assert.Equal(t, "1234567+octocat@users.noreply.github.com", email)
}

func TestGithub_CreateMergeRequest_HonorsContext(t *testing.T) {
	// A cancelled context must abort before any request is sent. This would have
	// passed silently when the implementation used context.TODO().
	client, _ := github.NewClient(nil).WithEnterpriseURLs("http://example.invalid", "")
	gh := &Github{client: client, owner: "o", repo: "r", fs: afero.NewMemMapFs()}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := gh.CreateMergeRequest(ctx, "Test MR", "body", "source", "target")
	assert.ErrorIs(t, err, context.Canceled)
}

func TestGithub_GetUser_HonorsContext(t *testing.T) {
	client, _ := github.NewClient(nil).WithEnterpriseURLs("http://example.invalid", "")
	gh := &Github{client: client, owner: "o", repo: "r", fs: afero.NewMemMapFs()}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// A request error (not the Actions 403 fallback) yields empty strings.
	name, email := gh.GetUser(ctx)
	assert.Empty(t, name)
	assert.Empty(t, email)
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
