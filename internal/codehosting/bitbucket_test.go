package codehosting

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBitbucket_ParseURL_Workspace(t *testing.T) {
	bb := newBitbucket("https://bitbucket.org/myworkspace/myrepo", "dummy-token")
	assert.Equal(t, "myworkspace", bb.workspace)
}

func TestBitbucket_ParseURL_RepoSlug(t *testing.T) {
	bb := newBitbucket("https://bitbucket.org/myworkspace/myrepo", "dummy-token")
	assert.Equal(t, "myrepo", bb.repoSlug)
}

func TestBitbucket_ParseURL_StripsDotGit(t *testing.T) {
	bb := newBitbucket("https://bitbucket.org/myworkspace/myrepo.git", "dummy-token")
	assert.Equal(t, "myrepo", bb.repoSlug)
}

func TestBitbucket_CreateMergeRequest_Success(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost && r.URL.Path == "/2.0/repositories/myworkspace/myrepo/pullrequests" {
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id": 42, "links": {"html": {"href": "https://bitbucket.org/myworkspace/myrepo/pull-requests/42"}}}`))
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mockServer.Close()

	bb := &Bitbucket{
		httpClient: &http.Client{},
		apiBaseURL: mockServer.URL + "/2.0",
		workspace:  "myworkspace",
		repoSlug:   "myrepo",
	}

	mr, err := bb.CreateMergeRequest(context.Background(), "Test PR", "description", "source-branch", "main")
	require.NoError(t, err)
	assert.Equal(t, int64(42), mr.ID)
	assert.Equal(t, "https://bitbucket.org/myworkspace/myrepo/pull-requests/42", mr.URL)
}

func TestBitbucket_CreateMergeRequest_Error(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error": {"message": "Branch does not exist"}}`))
	}))
	defer mockServer.Close()

	bb := &Bitbucket{
		httpClient: &http.Client{},
		apiBaseURL: mockServer.URL + "/2.0",
		workspace:  "myworkspace",
		repoSlug:   "myrepo",
	}

	_, err := bb.CreateMergeRequest(context.Background(), "Test PR", "description", "nonexistent", "main")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create pull request")
}

func TestBitbucket_CreateMergeRequest_HonorsContext(t *testing.T) {
	bb := &Bitbucket{
		httpClient: &http.Client{},
		apiBaseURL: "http://example.invalid/2.0",
		workspace:  "ws",
		repoSlug:   "repo",
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := bb.CreateMergeRequest(ctx, "Test PR", "body", "source", "target")
	require.ErrorIs(t, err, context.Canceled)
}

func TestBitbucket_DeleteBranch_Success(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && r.URL.Path == "/2.0/repositories/myworkspace/myrepo/refs/branches/update-abc123" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer mockServer.Close()

	bb := &Bitbucket{
		httpClient: &http.Client{},
		apiBaseURL: mockServer.URL + "/2.0",
		workspace:  "myworkspace",
		repoSlug:   "myrepo",
	}

	err := bb.DeleteBranch(context.Background(), "update-abc123")
	require.NoError(t, err)
}

func TestBitbucket_DeleteBranch_Error(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error": {"message": "Branch not found"}}`))
	}))
	defer mockServer.Close()

	bb := &Bitbucket{
		httpClient: &http.Client{},
		apiBaseURL: mockServer.URL + "/2.0",
		workspace:  "myworkspace",
		repoSlug:   "myrepo",
	}

	err := bb.DeleteBranch(context.Background(), "nonexistent-branch")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to delete branch")
}

func TestBitbucket_DeleteBranch_HonorsContext(t *testing.T) {
	bb := &Bitbucket{
		httpClient: &http.Client{},
		apiBaseURL: "http://example.invalid/2.0",
		workspace:  "ws",
		repoSlug:   "repo",
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := bb.DeleteBranch(ctx, "some-branch")
	require.ErrorIs(t, err, context.Canceled)
}

func TestBitbucket_GetUser_Success(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/2.0/user":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"display_name": "Jane Doe", "nickname": "janedoe"}`))
		case "/2.0/user/emails":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"values": [{"email": "jane@example.com", "is_primary": true}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mockServer.Close()

	bb := &Bitbucket{
		httpClient: &http.Client{},
		apiBaseURL: mockServer.URL + "/2.0",
		workspace:  "myworkspace",
		repoSlug:   "myrepo",
	}

	name, email := bb.GetUser(context.Background())
	assert.Equal(t, "Jane Doe", name)
	assert.Equal(t, "jane@example.com", email)
}

func TestBitbucket_GetUser_FallsBackToFirstEmail(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/2.0/user":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"display_name": "Jane Doe"}`))
		case "/2.0/user/emails":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"values": [{"email": "jane@example.com", "is_primary": false}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mockServer.Close()

	bb := &Bitbucket{
		httpClient: &http.Client{},
		apiBaseURL: mockServer.URL + "/2.0",
		workspace:  "myworkspace",
		repoSlug:   "myrepo",
	}

	name, email := bb.GetUser(context.Background())
	assert.Equal(t, "Jane Doe", name)
	assert.Equal(t, "jane@example.com", email)
}

func TestBitbucket_GetUser_ReturnsEmptyOnUserError(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer mockServer.Close()

	bb := &Bitbucket{
		httpClient: &http.Client{},
		apiBaseURL: mockServer.URL + "/2.0",
		workspace:  "myworkspace",
		repoSlug:   "myrepo",
	}

	name, email := bb.GetUser(context.Background())
	assert.Empty(t, name)
	assert.Empty(t, email)
}

func TestBitbucket_GetUser_ReturnsNameWhenEmailEndpointFails(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/2.0/user" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"display_name": "Jane Doe"}`))
		} else {
			w.WriteHeader(http.StatusForbidden)
		}
	}))
	defer mockServer.Close()

	bb := &Bitbucket{
		httpClient: &http.Client{},
		apiBaseURL: mockServer.URL + "/2.0",
		workspace:  "myworkspace",
		repoSlug:   "myrepo",
	}

	name, email := bb.GetUser(context.Background())
	assert.Equal(t, "Jane Doe", name)
	assert.Empty(t, email)
}

func TestBitbucket_GetUser_HonorsContext(t *testing.T) {
	bb := &Bitbucket{
		httpClient: &http.Client{},
		apiBaseURL: "http://example.invalid/2.0",
		workspace:  "ws",
		repoSlug:   "repo",
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	name, email := bb.GetUser(ctx)
	assert.Empty(t, name)
	assert.Empty(t, email)
}

func TestBitbucket_GetUser_ReturnsNameWhenNoEmails(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/2.0/user":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"display_name": "Jane Doe"}`))
		case "/2.0/user/emails":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"values": []}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mockServer.Close()

	bb := &Bitbucket{
		httpClient: &http.Client{},
		apiBaseURL: mockServer.URL + "/2.0",
		workspace:  "myworkspace",
		repoSlug:   "myrepo",
	}

	name, email := bb.GetUser(context.Background())
	assert.Equal(t, "Jane Doe", name)
	assert.Empty(t, email)
}

func TestBitbucket_GetUser_InvalidUserJSON(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{invalid json}`))
	}))
	defer mockServer.Close()

	bb := &Bitbucket{
		httpClient: &http.Client{},
		apiBaseURL: mockServer.URL + "/2.0",
		workspace:  "myworkspace",
		repoSlug:   "myrepo",
	}

	name, email := bb.GetUser(context.Background())
	assert.Empty(t, name)
	assert.Empty(t, email)
}

func TestBitbucket_GetUser_InvalidEmailJSON(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/2.0/user":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"display_name": "Jane Doe"}`))
		case "/2.0/user/emails":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{invalid json}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mockServer.Close()

	bb := &Bitbucket{
		httpClient: &http.Client{},
		apiBaseURL: mockServer.URL + "/2.0",
		workspace:  "myworkspace",
		repoSlug:   "myrepo",
	}

	name, email := bb.GetUser(context.Background())
	assert.Equal(t, "Jane Doe", name)
	assert.Empty(t, email)
}

func TestBitbucket_CreateMergeRequest_InvalidJSON(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{invalid json}`))
	}))
	defer mockServer.Close()

	bb := &Bitbucket{
		httpClient: &http.Client{},
		apiBaseURL: mockServer.URL + "/2.0",
		workspace:  "myworkspace",
		repoSlug:   "myrepo",
	}

	_, err := bb.CreateMergeRequest(context.Background(), "Test PR", "desc", "source", "main")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to decode pull request response")
}

func TestBitbucket_ParseURL_ShortPath(t *testing.T) {
	bb := newBitbucket("https://bitbucket.org/onlyone", "token")
	assert.Empty(t, bb.workspace)
	assert.Empty(t, bb.repoSlug)
}

func TestBitbucketTransport_RoundTrip_AddsAuthHeader(t *testing.T) {
	var capturedHeader string
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeader = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer mockServer.Close()

	transport := &bitbucketTransport{token: "my-secret-token"}
	client := &http.Client{Transport: transport}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, mockServer.URL, nil)
	require.NoError(t, err)
	resp, err := client.Do(req)
	require.NoError(t, err)
	resp.Body.Close()

	assert.Equal(t, "Bearer my-secret-token", capturedHeader)
}

func TestBitbucket_doRequest_MarshalError(t *testing.T) {
	bb := &Bitbucket{
		httpClient: &http.Client{},
		apiBaseURL: "http://example.com/2.0",
		workspace:  "ws",
		repoSlug:   "repo",
	}

	// channels cannot be JSON-marshaled
	_, err := bb.doRequest(context.Background(), http.MethodPost, "http://example.com", make(chan int))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to marshal request body")
}
