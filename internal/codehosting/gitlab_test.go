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

// newAutoMergeServer serves the GetMergeRequest endpoint with the given sequence of
// detailed_merge_status values (the last one repeats once exhausted) and accepts the merge.
// It returns the server plus counters for the GET and PUT calls.
func newAutoMergeServer(t *testing.T, statuses ...string) (*httptest.Server, *int, *int) {
	t.Helper()
	var getCount, putCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v4/projects/test_project/merge_requests/1":
			status := statuses[min(getCount, len(statuses)-1)]
			getCount++
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"iid":1,"detailed_merge_status":"` + status + `"}`))
		case r.Method == http.MethodPut && r.URL.Path == "/api/v4/projects/test_project/merge_requests/1/merge":
			putCount++
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"iid":1,"state":"merged"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	return server, &getCount, &putCount
}

func TestGitlab_EnableAutoMerge_WaitsOutPendingStatus(t *testing.T) {
	// GitLab computes mergeability asynchronously; the first reads are still pending.
	mockServer, getCount, putCount := newAutoMergeServer(t, "preparing", "checking", "mergeable")

	client, _ := gitlab.NewClient("", gitlab.WithBaseURL(mockServer.URL))
	g := &Gitlab{client: client, projectPath: "test_project", retryInterval: 0}

	err := g.EnableAutoMerge(context.Background(), MergeRequest{ID: 1})
	require.NoError(t, err)
	assert.Equal(t, 3, *getCount, "should poll while the merge status is still being computed")
	assert.Equal(t, 1, *putCount)
}

// TestGitlab_EnableAutoMerge_AcceptsWhileCIRunning is the regression test for the feature's
// central case: a pipeline-gated MR never reports "mergeable" before the pipeline finishes, so
// waiting for that status would make auto-merge unusable on exactly the projects that want it.
// A CI status is a settled answer and must be accepted straight away.
func TestGitlab_EnableAutoMerge_AcceptsWhileCIRunning(t *testing.T) {
	for _, status := range []string{"ci_still_running", "ci_must_pass"} {
		t.Run(status, func(t *testing.T) {
			mockServer, getCount, putCount := newAutoMergeServer(t, status)

			client, _ := gitlab.NewClient("", gitlab.WithBaseURL(mockServer.URL))
			g := &Gitlab{client: client, projectPath: "test_project", retryInterval: 0}

			err := g.EnableAutoMerge(context.Background(), MergeRequest{ID: 1})
			require.NoError(t, err)
			assert.Equal(t, 1, *getCount, "a CI status is settled — no reason to keep polling")
			assert.Equal(t, 1, *putCount, "auto-merge must be enabled while the pipeline runs")
		})
	}
}

func TestGitlab_EnableAutoMerge_StatusNeverSettles(t *testing.T) {
	mockServer, getCount, putCount := newAutoMergeServer(t, "checking")

	client, _ := gitlab.NewClient("", gitlab.WithBaseURL(mockServer.URL))
	g := &Gitlab{client: client, projectPath: "test_project", retryInterval: 0}

	err := g.EnableAutoMerge(context.Background(), MergeRequest{ID: 1})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "merge status was still pending after 7 checks")
	assert.Equal(t, maxMergeStatusChecks, *getCount)
	assert.Zero(t, *putCount, "must not attempt the merge when the status never settled")
}

func TestGitlab_EnableAutoMerge_MergeRetryOn405(t *testing.T) {
	putCount := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v4/projects/test_project/merge_requests/1":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"iid":1,"detailed_merge_status":"mergeable"}`))
		case r.Method == http.MethodPut && r.URL.Path == "/api/v4/projects/test_project/merge_requests/1/merge":
			putCount++
			if putCount < 3 {
				w.WriteHeader(http.StatusMethodNotAllowed)
				_, _ = w.Write([]byte(`{"message":"405 Method Not Allowed"}`))
			} else {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"iid":1,"state":"merged"}`))
			}
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mockServer.Close()

	client, _ := gitlab.NewClient("", gitlab.WithBaseURL(mockServer.URL))
	g := &Gitlab{client: client, projectPath: "test_project", retryInterval: 0}

	err := g.EnableAutoMerge(context.Background(), MergeRequest{ID: 1})
	require.NoError(t, err)
	assert.Equal(t, 3, putCount, "should retry merge on 405")
}

func TestGitlab_EnableAutoMerge_MergeFailsAfterMaxRetries(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v4/projects/test_project/merge_requests/1":
			w.WriteHeader(http.StatusOK)
			// GitLab documents 405 as "the merge request cannot merge"; a draft is one
			// settled status that produces it, and naming it makes the warning actionable.
			_, _ = w.Write([]byte(`{"iid":1,"detailed_merge_status":"draft_status"}`))
		case r.Method == http.MethodPut && r.URL.Path == "/api/v4/projects/test_project/merge_requests/1/merge":
			w.WriteHeader(http.StatusMethodNotAllowed)
			_, _ = w.Write([]byte(`{"message":"405 Method Not Allowed"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mockServer.Close()

	client, _ := gitlab.NewClient("", gitlab.WithBaseURL(mockServer.URL))
	g := &Gitlab{client: client, projectPath: "test_project", retryInterval: 0}

	err := g.EnableAutoMerge(context.Background(), MergeRequest{ID: 1})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "could not set auto merge for MR 1")
	assert.Contains(t, err.Error(), `draft_status`, "the settled status names the real blocker")
}

func TestGitlab_EnableAutoMerge_GetMRError(t *testing.T) {
	// Use 403 (not 5xx) to avoid retries by the underlying retryable HTTP client.
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"403 Forbidden"}`))
	}))
	defer mockServer.Close()

	client, _ := gitlab.NewClient("bad-token", gitlab.WithBaseURL(mockServer.URL))
	g := &Gitlab{client: client, projectPath: "test_project", retryInterval: 0}

	err := g.EnableAutoMerge(context.Background(), MergeRequest{ID: 1})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "could not set auto merge for MR 1")
}

func TestGitlab_EnableAutoMerge_HonorsContext(t *testing.T) {
	g := newTestGitlab(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := g.EnableAutoMerge(ctx, MergeRequest{ID: 1})
	require.ErrorIs(t, err, context.Canceled)
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
