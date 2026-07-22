package codehosting

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"

	"go.uber.org/zap"
)

// Platform defines the interface for version control system providers.
type Platform interface {
	// CreateMergeRequest creates a merge/pull request on the platform.
	CreateMergeRequest(ctx context.Context, title string, description string, sourceBranch string, targetBranch string) (MergeRequest, error)

	// DeleteBranch removes a remote branch from the platform.
	DeleteBranch(ctx context.Context, branch string) error

	// GetUser returns the user name and email from the platform.
	GetUser(ctx context.Context) (name string, email string)

	// EnableAutoMerge sets the merge/pull request to merge automatically when all conditions are met.
	EnableAutoMerge(ctx context.Context, mr MergeRequest) error
}

// MergeRequest represents a merge or pull request on a code hosting platform.
type MergeRequest struct {
	ID  int64  `json:"id"`
	URL string `json:"url"`
}

// DefaultVcsProviderFactory creates appropriate Platform implementations
// based on repository URLs.
type DefaultVcsProviderFactory struct{}

// NewDefaultVcsProviderFactory creates a new factory for VCS providers.
func NewDefaultVcsProviderFactory() *DefaultVcsProviderFactory {
	return &DefaultVcsProviderFactory{}
}

// Create returns a Platform implementation appropriate for the given repository URL.
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

// providerFromEnv returns the VCS provider from CI environment variables, which are
// authoritative and work for self-hosted instances whose hostnames don't contain the
// provider name. Returns "" when no CI environment is detected.
func providerFromEnv() string {
	if os.Getenv("GITHUB_ACTIONS") == "true" {
		return "github"
	}
	if os.Getenv("GITLAB_CI") == "true" {
		return "gitlab"
	}
	return ""
}

// providerFromHost determines the VCS provider type from the repository host. Matching on the
// host (not the whole URL) avoids misrouting a GitHub repo whose owner/name contains "gitlab"
// (e.g. github.com/foo/gitlab-migration).
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

// parseGitURL extracts the host and "owner/repo" path from a repository URL, accepting both
// HTTP(S) URLs (https://host/owner/repo.git) and SCP-style git URLs (git@host:owner/repo.git).
// Any trailing ".git" and surrounding slashes are stripped.
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
