package drupalorg

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// drupalCodeServer answers each route with its body as JSON, and anything else with 404.
func drupalCodeServer(t *testing.T, routes map[string]any) (*DrupalCode, *[]*http.Request) {
	t.Helper()
	var requests []*http.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r)
		body, ok := routes[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		assert.NoError(t, json.NewEncoder(w).Encode(body))
	}))
	t.Cleanup(server.Close)

	d, err := newDrupalCode("secret", server.URL+"/api/v4")
	require.NoError(t, err)
	return d, &requests
}

func TestNewDrupalCodeTalksToDrupalCode(t *testing.T) {
	d, err := NewDrupalCode("secret")
	require.NoError(t, err)
	assert.Equal(t, "https://git.drupalcode.org/api/v4/", d.client.BaseURL().String())
}

func TestIsFixedIn(t *testing.T) {
	const search = "/api/v4/projects/project/drupal/-/search"

	t.Run("a commit naming the issue on the ref is a fix", func(t *testing.T) {
		d, requests := drupalCodeServer(t, map[string]any{search: []map[string]string{{"id": "5678"}}})

		fixed, err := d.IsFixedIn(t.Context(), "drupal", "123456", "11.1.0")
		require.NoError(t, err)
		assert.True(t, fixed)

		require.Len(t, *requests, 1)
		query := (*requests)[0].URL.Query()
		assert.Equal(t, "123456", query.Get("search"))
		assert.Equal(t, "11.1.0", query.Get("ref"))
		assert.Equal(t, "commits", query.Get("scope"))
		assert.Equal(t, "secret", (*requests)[0].Header.Get("Private-Token"))
	})

	t.Run("no such commit is no fix", func(t *testing.T) {
		d, _ := drupalCodeServer(t, map[string]any{search: []map[string]string{}})

		fixed, err := d.IsFixedIn(t.Context(), "drupal", "123456", "11.1.0")
		require.NoError(t, err)
		assert.False(t, fixed)
	})

	t.Run("a failed search is an error, not a missing fix", func(t *testing.T) {
		d, _ := drupalCodeServer(t, nil)

		_, err := d.IsFixedIn(t.Context(), "drupal", "123456", "11.1.0")
		require.ErrorContains(t, err, "failed to search drupal for issue 123456")
	})
}

func TestIssueForkMergeRequests(t *testing.T) {
	const (
		fork          = "/api/v4/projects/issue/drupal-123456"
		mergeRequests = "/api/v4/projects/project/drupal/merge_requests"
	)

	t.Run("lists the merge requests from the issue fork only", func(t *testing.T) {
		d, requests := drupalCodeServer(t, map[string]any{
			fork: map[string]any{"id": 5678},
			mergeRequests: []map[string]any{
				{"sha": "111111", "web_url": "https://git.drupalcode.org/project/drupal/-/merge_requests/2"},
				{"sha": "222222", "web_url": "https://git.drupalcode.org/project/drupal/-/merge_requests/1"},
			},
		})

		got, err := d.IssueForkMergeRequests(t.Context(), "drupal", "123456")
		require.NoError(t, err)
		assert.Equal(t, []ForkMergeRequest{
			{SHA: "111111", WebURL: "https://git.drupalcode.org/project/drupal/-/merge_requests/2"},
			{SHA: "222222", WebURL: "https://git.drupalcode.org/project/drupal/-/merge_requests/1"},
		}, got)

		require.Len(t, *requests, 2)
		assert.Equal(t, "5678", (*requests)[1].URL.Query().Get("source_project_id"),
			"without the filter every merge request into the project would be taken for the fork's")
	})

	t.Run("an issue without a fork is an error", func(t *testing.T) {
		d, requests := drupalCodeServer(t, nil)

		_, err := d.IssueForkMergeRequests(t.Context(), "drupal", "123456")
		require.ErrorContains(t, err, "failed to get the fork of issue 123456")
		assert.Len(t, *requests, 1, "no merge request query without a fork to filter on")
	})

	t.Run("a failed merge request query is an error", func(t *testing.T) {
		d, _ := drupalCodeServer(t, map[string]any{fork: map[string]any{"id": 5678}})

		_, err := d.IssueForkMergeRequests(t.Context(), "drupal", "123456")
		require.ErrorContains(t, err, "failed to list the merge requests from the fork of issue 123456")
	})

	t.Run("a fork with no merge requests yields none", func(t *testing.T) {
		d, _ := drupalCodeServer(t, map[string]any{
			fork:          map[string]any{"id": 5678},
			mergeRequests: []map[string]any{},
		})

		got, err := d.IssueForkMergeRequests(t.Context(), "drupal", "123456")
		require.NoError(t, err)
		assert.Empty(t, got)
	})
}
