package codehosting

import (
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
	mr, err := gh.CreateMergeRequest("Test MR", "This is a test MR", "source-branch", "target-branch")

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, 1, mr.ID)
	assert.Equal(t, "http://example.com", mr.URL)
}

func TestGithub_DownloadComposerFiles(t *testing.T) {
	// Setup mock HTTP server for file content
	mockContentServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte(`[{"name":"composer.json","download_url":""}]`))
		assert.NoError(t, err)
	}))
	defer mockContentServer.Close()

	// Setup mock HTTP server for repository contents
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.URL.Path == "/api/v3/repos/test_owner/test_project/contents/" {
			jsonResponse := []byte(`[
				{"name":"composer.json","download_url":"` + mockContentServer.URL + `"}, 
				{"name":"composer.lock","download_url":"` + mockContentServer.URL + `"}
			]`)
			w.WriteHeader(http.StatusOK)
			_, err := w.Write(jsonResponse)
			assert.NoError(t, err)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mockServer.Close()

	// Create GitHub client with mock server
	client, _ := github.NewClient(nil).WithEnterpriseURLs(mockServer.URL, "")

	gh := &Github{
		client: client,
		owner:  "test_owner",
		repo:   "test_project",
		fs:     afero.NewMemMapFs(),
	}

	// Execute
	dir := gh.DownloadComposerFiles("main")

	// Assert
	assert.NotEmpty(t, dir)

	// Verify files were created in the filesystem
	_, err := gh.fs.Stat(dir)
	assert.NoError(t, err)

	_, err = gh.fs.Stat(dir + "/composer.json")
	assert.NoError(t, err)

	_, err = gh.fs.Stat(dir + "/composer.lock")
	assert.NoError(t, err)
}
