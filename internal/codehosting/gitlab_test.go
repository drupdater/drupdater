package codehosting

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gitlab "gitlab.com/gitlab-org/api/client-go"
	"go.uber.org/zap"
)

func newTestGitlab(t *testing.T) *Gitlab {
	t.Helper()
	g, err := newGitlab("gitlab.com", "user/repo", "dummy-token", zap.NewNop())
	require.NoError(t, err)
	return g
}

func TestGitlab_CreateMergeRequest(t *testing.T) {

	gitlab := newTestGitlab(t)

	title := "Test MR"
	sourceBranch := "feature-branch"
	targetBranch := "main"
	description := "Test MR description"

	t.Run("failed to get create mr", func(t *testing.T) {

		_, err := gitlab.CreateMergeRequest(context.Background(), title, description, sourceBranch, targetBranch)
		require.Error(t, err)
	})

}

func TestGitlab_getBaseUrl(t *testing.T) {
	g := newTestGitlab(t)
	assert.Equal(t, "gitlab.com", g.client.BaseURL().Host)
}

func TestNewGitlab_MissingHost(t *testing.T) {
	_, err := newGitlab("", "user/repo", "dummy-token", zap.NewNop())
	require.Error(t, err)
}

func TestCreateMergeRequest(t *testing.T) {

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		jsonString := make([]byte, 0)
		if r.URL.Path == "/api/v4/projects/test_project/merge_requests" {
			jsonString = []byte(`{"iid": 1, "web_url": "http://example.com"}`)
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}

		_, err := w.Write(jsonString)
		assert.NoError(t, err)
	}))
	defer mockServer.Close()

	client, _ := gitlab.NewClient("", gitlab.WithBaseURL(mockServer.URL))

	gitlab := &Gitlab{
		client:      client,
		projectPath: "test_project",
	}

	mr, err := gitlab.CreateMergeRequest(context.Background(), "Test MR", "This is a test MR", "source-branch", "target-branch")
	require.NoError(t, err)
	assert.Equal(t, int64(1), mr.ID)
	assert.Equal(t, "http://example.com", mr.URL)
}

func TestGitlab_CreateMergeRequest_HonorsContext(t *testing.T) {
	// A cancelled context must abort before the request is sent.
	g := newTestGitlab(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := g.CreateMergeRequest(ctx, "Test MR", "body", "source", "target")
	require.ErrorIs(t, err, context.Canceled)
}

func TestGitlab_DeleteBranch_Success(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && r.URL.Path == "/api/v4/projects/test_project/repository/branches/update-abc123" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer mockServer.Close()

	client, _ := gitlab.NewClient("", gitlab.WithBaseURL(mockServer.URL))
	g := &Gitlab{client: client, projectPath: "test_project"}

	err := g.DeleteBranch(context.Background(), "update-abc123")
	require.NoError(t, err)
}

func TestGitlab_DeleteBranch_Error(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"404 Branch Not Found"}`))
	}))
	defer mockServer.Close()

	client, _ := gitlab.NewClient("", gitlab.WithBaseURL(mockServer.URL))
	g := &Gitlab{client: client, projectPath: "test_project"}

	err := g.DeleteBranch(context.Background(), "nonexistent-branch")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to delete branch")
}

func TestGitlab_DeleteBranch_HonorsContext(t *testing.T) {
	g := newTestGitlab(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := g.DeleteBranch(ctx, "some-branch")
	require.ErrorIs(t, err, context.Canceled)
}

func TestGitlab_GetUser_HonorsContext(t *testing.T) {
	g := newTestGitlab(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	name, email := g.GetUser(ctx)
	assert.Empty(t, name)
	assert.Empty(t, email)
}

func TestGetUser_ReturnsEmptyStringsOnError(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer mockServer.Close()

	client, _ := gitlab.NewClient("bad-token", gitlab.WithBaseURL(mockServer.URL))

	g := &Gitlab{
		client:      client,
		projectPath: "test_project",
	}

	name, email := g.GetUser(context.Background())
	assert.Empty(t, name)
	assert.Empty(t, email)
}
