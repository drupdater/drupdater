package codehosting

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"

	"go.uber.org/zap"
)

// Platform is a version control hosting provider.
type Platform interface {
	CreateMergeRequest(ctx context.Context, title string, description string, sourceBranch string, targetBranch string) (MergeRequest, error)

	DeleteBranch(ctx context.Context, branch string) error

	GetUser(ctx context.Context) (name string, email string)

	// EnableAutoMerge merges once every condition the platform enforces is met.
	EnableAutoMerge(ctx context.Context, mr MergeRequest) error
}

type MergeRequest struct {
	ID  int64  `json:"id"`
	URL string `json:"url"`
}

type DefaultVcsProviderFactory struct{}

func NewDefaultVcsProviderFactory() *DefaultVcsProviderFactory {
	return &DefaultVcsProviderFactory{}
}

// Create returns the Platform implementation for a repository URL.
func (vpf *DefaultVcsProviderFactory) Create(repositoryURL string, token string, logger *zap.Logger) (Platform, error) {
	host, path, err := parseGitURL(repositoryURL)
	if err != nil {
		return nil, err
	}

	provider := providerFromEnv()
	if provider == "" {
		provider = providerFromHost(host)
	}

	switch provider {
	case "github":
		return newGithub(path, token, logger)
	default:
		return newGitlab(host, path, token, logger)
	}
}

// providerFromEnv reads the provider from CI environment variables, which are authoritative and
// work for self-hosted instances whose hostname does not name the provider. "" when not in CI.
func providerFromEnv() string {
	if os.Getenv("GITHUB_ACTIONS") == "true" {
		return "github"
	}
	if os.Getenv("GITLAB_CI") == "true" {
		return "gitlab"
	}
	return ""
}

// providerFromHost matches on the host, not the whole URL, so github.com/foo/gitlab-migration
// is not misrouted to GitLab.
func providerFromHost(host string) string {
	host = strings.ToLower(host)
	switch {
	case strings.Contains(host, "gitlab"):
		return "gitlab"
	case strings.Contains(host, "github"):
		return "github"
	default:
		return ""
	}
}

// ValidateRepositoryURL reports whether raw is a URL this tool can route to a provider, so
// callers validate against what Create accepts — including scheme-less SCP-style git URLs.
func ValidateRepositoryURL(raw string) error {
	_, _, err := parseGitURL(raw)
	return err
}

// parseGitURL extracts the host and "owner/repo" from either an HTTP(S) URL or an SCP-style git
// URL (git@host:owner/repo.git), stripping any trailing ".git".
func parseGitURL(raw string) (host string, path string, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", fmt.Errorf("empty repository URL")
	}

	// SCP-like syntax has no scheme: [user@]host:owner/repo.
	if !strings.Contains(raw, "://") {
		if i := strings.LastIndex(raw, "@"); i >= 0 {
			raw = raw[i+1:]
		}
		host, path, found := strings.Cut(raw, ":")
		if !found || host == "" || path == "" {
			return "", "", fmt.Errorf("invalid repository URL %q", raw)
		}
		return host, strings.TrimSuffix(strings.Trim(path, "/"), ".git"), nil
	}

	u, err := url.Parse(raw)
	if err != nil {
		return "", "", fmt.Errorf("invalid repository URL %q: %w", raw, err)
	}
	if u.Host == "" {
		return "", "", fmt.Errorf("invalid repository URL %q: missing host", raw)
	}
	return u.Host, strings.TrimSuffix(strings.Trim(u.Path, "/"), ".git"), nil
}
