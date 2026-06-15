package codehosting

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	gitlab "gitlab.com/gitlab-org/api/client-go"
)

func TestGitlab_CreateMergeRequest(t *testing.T) {

	gitlab := newGitlab("https://gitlab.com/user/repo", "dummy-token")

	title := "Test MR"
	sourceBranch := "feature-branch"
	targetBranch := "main"
	description := "Test MR description"

	t.Run("failed to get create mr", func(t *testing.T) {

		_, err := gitlab.CreateMergeRequest(title, description, sourceBranch, targetBranch)
		assert.Error(t, err)
	})

}

func TestGitlab_getBaseUrl(t *testing.T) {

	tt := []string{"https://gitlab.com/user/repo", "https://gitlab.com/user/repo.git", "https://gitlab.com/user/repo/", "https://gitlab.com/group/user/repo.git"}

	for _, url := range tt {
		gitlab := newGitlab(url, "dummy-token")

		assert.Equal(t, "gitlab.com", gitlab.client.BaseURL().Host)
	}
}

func TestGitlab_getProjectPath(t *testing.T) {

	tt := []string{"https://gitlab.com/user/repo", "https://gitlab.com/user/repo.git", "https://gitlab.com/user/repo/"}

	for _, url := range tt {

		gitlab := newGitlab(url, "dummy-token")

		expectedProjectPath := "user/repo"
		assert.Equal(t, expectedProjectPath, gitlab.projectPath)
	}
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
		fs:          afero.NewMemMapFs(),
	}

	mr, err := gitlab.CreateMergeRequest("Test MR", "This is a test MR", "source-branch", "target-branch")
	assert.NoError(t, err)
	assert.Equal(t, 1, mr.ID)
	assert.Equal(t, "http://example.com", mr.URL)
}

func TestDownloadComposerFiles(t *testing.T) {

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		jsonString := make([]byte, 0)
		switch r.URL.Path {
		case "/api/v4/projects/test_project/repository/files/composer.json/raw":
			jsonString = []byte("{}")
			w.WriteHeader(http.StatusOK)
		case "/api/v4/projects/test_project/repository/files/composer.lock/raw":
			jsonString = []byte("{}")
			w.WriteHeader(http.StatusOK)
		default:
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
		fs:          afero.NewMemMapFs(),
	}

	dir := gitlab.DownloadComposerFiles("main")
	assert.NotEmpty(t, dir)

	_, err := gitlab.fs.Stat(dir)
	assert.NoError(t, err)

	_, err = gitlab.fs.Stat(dir + "/composer.json")
	assert.NoError(t, err)

	_, err = gitlab.fs.Stat(dir + "/composer.lock")
	assert.NoError(t, err)
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
		fs:          afero.NewMemMapFs(),
	}

	name, email := g.GetUser()
	assert.Equal(t, "", name)
	assert.Equal(t, "", email)
}

func TestDownloadComposerFiles_ReturnsEmptyOnComposerJsonError(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer mockServer.Close()

	client, _ := gitlab.NewClient("", gitlab.WithBaseURL(mockServer.URL))

	g := &Gitlab{
		client:      client,
		projectPath: "test_project",
		fs:          afero.NewMemMapFs(),
	}

	dir := g.DownloadComposerFiles("main")
	assert.Empty(t, dir)
}

func TestDownloadComposerFiles_ReturnsEmptyOnComposerLockError(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v4/projects/test_project/repository/files/composer.json/raw":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("{}"))
		default:
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer mockServer.Close()

	client, _ := gitlab.NewClient("", gitlab.WithBaseURL(mockServer.URL))

	g := &Gitlab{
		client:      client,
		projectPath: "test_project",
		fs:          afero.NewMemMapFs(),
	}

	dir := g.DownloadComposerFiles("main")
	assert.Empty(t, dir)
}

func TestDownloadComposerFiles_ReturnsEmptyOnTempDirError(t *testing.T) {
	client, _ := gitlab.NewClient("", gitlab.WithBaseURL("http://localhost"))

	g := &Gitlab{
		client:      client,
		projectPath: "test_project",
		fs:          afero.NewReadOnlyFs(afero.NewMemMapFs()),
	}

	dir := g.DownloadComposerFiles("main")
	assert.Empty(t, dir)
}

func TestDownloadAndWriteFile_ReturnsErrorOnNonOKStatus(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// 201 is a 2xx success so the client won't return an error, but it's not 200
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("{}"))
	}))
	defer mockServer.Close()

	client, _ := gitlab.NewClient("", gitlab.WithBaseURL(mockServer.URL))

	g := &Gitlab{
		client:      client,
		projectPath: "test_project",
		fs:          afero.NewMemMapFs(),
	}

	err := g.downloadAndWriteFile("main", "composer.json", "/tmp/dir")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP status")
}

func TestDownloadAndWriteFile_ReturnsErrorOnWriteFileFailure(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{}"))
	}))
	defer mockServer.Close()

	client, _ := gitlab.NewClient("", gitlab.WithBaseURL(mockServer.URL))

	g := &Gitlab{
		client:      client,
		projectPath: "test_project",
		fs:          afero.NewReadOnlyFs(afero.NewMemMapFs()),
	}

	err := g.downloadAndWriteFile("main", "composer.json", "/tmp/dir")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to write")
}
