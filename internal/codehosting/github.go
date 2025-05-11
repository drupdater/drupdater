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

func (g Github) CreateMergeRequest(title string, description string, sourceBranch string, targetBranch string) (MergeRequest, error) {
	mr, _, err := g.client.PullRequests.Create(context.TODO(), g.owner, g.repo, &github.NewPullRequest{
		Head:  &sourceBranch,
		Base:  &targetBranch,
		Title: &title,
		Body:  &description,
	})

	if err != nil {
		return MergeRequest{}, fmt.Errorf("failed to create pull request: %w", err)
	}
	return MergeRequest{
		ID:  mr.GetNumber(),
		URL: mr.GetHTMLURL(),
	}, nil
}

// DownloadComposerFiles downloads composer.json and composer.lock files from the given branch
// and returns the path to the temporary directory containing them.
func (g *Github) DownloadComposerFiles(branch string) string {
	dir, err := afero.TempDir(g.fs, "", "composer")
	if err != nil {
		// Better error handling instead of panic
		g.logError(fmt.Errorf("failed to create temp directory: %w", err))
		return ""
	}

	if err := g.downloadAndWriteFile(branch, "composer.json", dir); err != nil {
		return dir
	}
	if err := g.downloadAndWriteFile(branch, "composer.lock", dir); err != nil {
		return dir
	}

	return dir
}

// downloadAndWriteFile downloads a file from the repository and writes it to the given directory.
// Returns an error if the download or write operation fails.
func (g *Github) downloadAndWriteFile(branch, file, dir string) error {
	content, resp, err := g.client.Repositories.DownloadContents(
		context.Background(),
		g.owner,
		g.repo,
		file,
		&github.RepositoryContentGetOptions{
			Ref: branch,
		},
	)

	if err != nil {
		return fmt.Errorf("failed to download %s: %w", file, err)
	}

	if resp.StatusCode != 200 {
		return fmt.Errorf("failed to download %s: HTTP status %s", file, resp.Status)
	}

	err = afero.WriteReader(g.fs, dir+"/"+file, content)
	if err != nil {
		return fmt.Errorf("failed to write %s: %w", file, err)
	}

	return nil
}

// logError logs an error. This is a placeholder for proper error handling.
func (g *Github) logError(err error) {
	// In a real implementation, this would use a proper logging mechanism
	fmt.Printf("Error: %v\n", err)
}

// GetUser returns the name and email of the authenticated user.
func (g *Github) GetUser() (name string, email string) {
	user, _, err := g.client.Users.Get(context.Background(), "")
	if err != nil {
		// Better error handling instead of panic
		g.logError(fmt.Errorf("failed to get user: %w", err))
		return "Unknown", "unknown@example.com"
	}

	return user.GetName(), user.GetEmail()
}
