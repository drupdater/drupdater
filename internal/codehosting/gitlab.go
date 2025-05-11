package codehosting

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/spf13/afero"
	gitlab "gitlab.com/gitlab-org/api/client-go"
)

// Gitlab implements the Platform interface for GitLab repositories.
type Gitlab struct {
	client      *gitlab.Client
	projectPath string
	fs          afero.Fs
}

// newGitlab creates a new GitLab client based on repository URL and token.
func newGitlab(repositoryURL string, token string) *Gitlab {
	u, err := url.Parse(repositoryURL)
	if err != nil {
		// Handle URL parsing error properly
		return &Gitlab{
			fs: afero.NewOsFs(),
		}
	}

	baseURL := fmt.Sprintf("%s://%s", u.Scheme, u.Host)

	gitlabClient, err := gitlab.NewClient(token, gitlab.WithBaseURL(baseURL))
	if err != nil {
		// Log error instead of panic
		fmt.Printf("Error creating GitLab client: %v\n", err)
		return &Gitlab{
			fs: afero.NewOsFs(),
		}
	}

	return &Gitlab{
		client:      gitlabClient,
		projectPath: strings.TrimSuffix(strings.Trim(u.Path, "/"), ".git"),
		fs:          afero.NewOsFs(),
	}
}

// CreateMergeRequest creates a merge request on GitLab.
func (g *Gitlab) CreateMergeRequest(title string, description string, sourceBranch string, targetBranch string) (MergeRequest, error) {
	mr, _, err := g.client.MergeRequests.CreateMergeRequest(g.projectPath, &gitlab.CreateMergeRequestOptions{
		SourceBranch: &sourceBranch,
		TargetBranch: &targetBranch,
		Title:        &title,
		Description:  &description,
	})

	if err != nil {
		return MergeRequest{}, fmt.Errorf("failed to create merge request: %w", err)
	}

	return MergeRequest{
		ID:  mr.IID,
		URL: mr.WebURL,
	}, nil
}

// DownloadComposerFiles downloads composer.json and composer.lock files from the given branch.
func (g *Gitlab) DownloadComposerFiles(branch string) string {
	dir, err := afero.TempDir(g.fs, "", "composer")
	if err != nil {
		panic(err)
	}

	g.downloadAndWriteFile(branch, "composer.json", dir)
	g.downloadAndWriteFile(branch, "composer.lock", dir)

	return dir
}

func (g *Gitlab) downloadAndWriteFile(branch string, file string, dir string) {
	content, resp, err := g.client.RepositoryFiles.GetRawFile(g.projectPath, file, &gitlab.GetRawFileOptions{
		Ref: &branch,
	})

	if err != nil {
		panic(err)
	}

	if resp.StatusCode != 200 {
		panic(fmt.Errorf("failed to download file: %s", resp.Status))
	}

	err = afero.WriteFile(g.fs, dir+"/"+file, content, 0644)
	if err != nil {
		panic(err)
	}
}

func (g *Gitlab) GetUser() (name string, email string) {
	user, _, err := g.client.Users.CurrentUser()
	if err != nil {
		panic(err)
	}

	return user.Name, user.Email
}
