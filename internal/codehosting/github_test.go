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
	github := newGithub("https://github.com/owner/repo", "dummy-token")

	assert.Equal(t, "owner", github.owner)
}

func TestGithub_GetRepo(t *testing.T) {

	github := newGithub("https://github.com/owner/repo", "dummy-token")

	assert.Equal(t, "repo", github.repo)
}

func TestGithub_CreateMergeRequest(t *testing.T) {

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		jsonString := make([]byte, 0)
		if r.URL.Path == "/api/v3/repos/test_owner/test_project/pulls" {
			jsonString = []byte("{}")
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}

		_, err := w.Write(jsonString)
		assert.NoError(t, err)
	}))
	defer mockServer.Close()

	client, _ := github.NewClient(nil).WithEnterpriseURLs(mockServer.URL, "")

	github := &Github{
		client: client,
		owner:  "test_owner",
		repo:   "test_project",
		fs:     afero.NewMemMapFs(),
	}

	err := github.CreateMergeRequest("Test MR", "This is a test MR", "source-branch", "target-branch")
	assert.NoError(t, err)
}

func TestGithub_DownloadComposerFiles(t *testing.T) {
	mockServer1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		jsonString := make([]byte, 0)
		if r.URL.Path == "/" {
			jsonString = []byte(`[{"name":"composer.json","download_url":""}]`)
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}

		_, err := w.Write(jsonString)
		assert.NoError(t, err)
	}))
	defer mockServer1.Close()

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		jsonString := make([]byte, 0)
		if r.URL.Path == "/api/v3/repos/test_owner/test_project/contents/" {
			jsonString = []byte(`[{"name":"composer.json","download_url":"` + mockServer1.URL + `"}, {"name":"composer.lock","download_url":"` + mockServer1.URL + `"}]`)
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}

		_, err := w.Write(jsonString)
		assert.NoError(t, err)
	}))
	defer mockServer.Close()

	client, _ := github.NewClient(nil).WithEnterpriseURLs(mockServer.URL, "")

	github := &Github{
		client: client,
		owner:  "test_owner",
		repo:   "test_project",
		fs:     afero.NewMemMapFs(),
	}

	dir := github.DownloadComposerFiles("main")
	assert.NotEmpty(t, dir)

	_, err := github.fs.Stat(dir)
	assert.NoError(t, err)

	_, err = github.fs.Stat(dir + "/composer.json")
	assert.NoError(t, err)

	_, err = github.fs.Stat(dir + "/composer.lock")
	assert.NoError(t, err)
}
