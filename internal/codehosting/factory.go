package codehosting

import (
	"strings"
)

type VcsProviderFactory interface {
	Create(repositoryURL string, token string) Platform
}

type DefaultVcsProviderFactory struct {
}

func NewDefaultVcsProviderFactory() *DefaultVcsProviderFactory {
	return &DefaultVcsProviderFactory{}
}

func (vpf *DefaultVcsProviderFactory) Create(repositoryURL string, token string) Platform {
	switch vpf.getProvider(repositoryURL) {
	case "gitlab":
		return newGitlab(repositoryURL, token)
	case "github":
		return newGithub(repositoryURL, token)
	default:
		return nil
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
	CreateMergeRequest(title string, description string, sourceBranch string, targetBranch string) error
	DownloadComposerFiles(branch string) string
}
