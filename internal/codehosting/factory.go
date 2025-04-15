package codehosting

import (
	"strings"
)

type VcsProviderFactory interface {
	Create(repositoryURL string, token string) Platform
}

type DefaultVcsProviderFactory struct {
}

type MergeRequest struct {
	ID  int    `json:"id"`
	URL string `json:"url"`
}

func NewDefaultVcsProviderFactory() *DefaultVcsProviderFactory {
	return &DefaultVcsProviderFactory{}
}

func (vpf *DefaultVcsProviderFactory) Create(repositoryURL string, token string) Platform {
	switch vpf.getProvider(repositoryURL) {
	case "github":
		return newGithub(repositoryURL, token)
	default:
		return newGitlab(repositoryURL, token)
	}
}
func (vpf *DefaultVcsProviderFactory) getProvider(repositoryURL string) string {
	if strings.Contains(repositoryURL, "gitlab") {
		return "gitlab"
	}
	if strings.Contains(repositoryURL, "github") {
		return "github"
	}
	return ""
}

type Platform interface {
	CreateMergeRequest(title string, description string, sourceBranch string, targetBranch string) (MergeRequest, error)
	DownloadComposerFiles(branch string) string
}
