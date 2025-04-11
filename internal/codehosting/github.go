package codehosting

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/google/go-github/v68/github"
	"github.com/spf13/afero"
)

type Github struct {
	client *github.Client
	owner  string
	repo   string
	fs     afero.Fs
}

func newGithub(repositoryURL string, token string) *Github {

	u, _ := url.Parse(repositoryURL)

	return &Github{
		client: github.NewClient(nil).WithAuthToken(token),
		owner:  strings.Split(u.Path, "/")[1],
		repo:   strings.Split(u.Path, "/")[2],
		fs:     afero.NewOsFs(),
	}
}

func (g Github) CreateMergeRequest(title string, description string, sourceBranch string, targetBranch string) error {
	_, _, err := g.client.PullRequests.Create(context.TODO(), g.owner, g.repo, &github.NewPullRequest{
		Head:  &sourceBranch,
		Base:  &targetBranch,
		Title: &title,
		Body:  &description,
	})
	return err
}

func (g Github) DownloadComposerFiles(branch string) string {

	dir, err := afero.TempDir(g.fs, "", "composer")
	if err != nil {
		panic(err)
	}

	g.downloadAndWriteFile(branch, "composer.json", dir)
	g.downloadAndWriteFile(branch, "composer.lock", dir)

	return dir
}

func (g Github) downloadAndWriteFile(branch string, file string, dir string) {

	content, resp, err := g.client.Repositories.DownloadContents(context.TODO(), g.owner, g.repo, file, &github.RepositoryContentGetOptions{
		Ref: branch,
	})

	if err != nil {
		panic(err)
	}

	if resp.StatusCode != 200 {
		panic(fmt.Errorf("failed to download file: %s", resp.Status))
	}

	err = afero.WriteReader(g.fs, dir+"/"+file, content)
	if err != nil {
		panic(err)
	}

}
