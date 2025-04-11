package codehosting

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/spf13/afero"
	gitlab "gitlab.com/gitlab-org/api/client-go"
)

type Gitlab struct {
	client      *gitlab.Client
	projectPath string
	fs          afero.Fs
}

func newGitlab(repositoryURL string, token string) *Gitlab {

	u, _ := url.Parse(repositoryURL)
	baseURL := fmt.Sprintf("%s://%s", u.Scheme, u.Host)

	gitlabClient, err := gitlab.NewClient(token, gitlab.WithBaseURL(baseURL))
	if err != nil {
		panic(err)
	}

	return &Gitlab{
		client:      gitlabClient,
		projectPath: strings.TrimSuffix(strings.Trim(u.Path, "/"), ".git"),
		fs:          afero.NewOsFs(),
	}
}

func (g Gitlab) CreateMergeRequest(title string, description string, sourceBranch string, targetBranch string) error {
	_, _, err := g.client.MergeRequests.CreateMergeRequest(g.projectPath, &gitlab.CreateMergeRequestOptions{
		SourceBranch: &sourceBranch,
		TargetBranch: &targetBranch,
		Title:        &title,
		Description:  &description,
	})
	return err
}

func (g Gitlab) DownloadComposerFiles(branch string) string {

	dir, err := afero.TempDir(g.fs, "", "composer")
	if err != nil {
		panic(err)
	}

	g.downloadAndWriteFile(branch, "composer.json", dir)
	g.downloadAndWriteFile(branch, "composer.lock", dir)

	return dir
}

func (g Gitlab) downloadAndWriteFile(branch string, file string, dir string) {

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
