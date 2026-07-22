package codehosting

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestDefaultVcsProviderFactory_Create(t *testing.T) {
	tests := []struct {
		name          string
		repositoryURL string
		expectedType  any
	}{
		{
			name:          "returns gitlab platform for gitlab URLs",
			repositoryURL: "https://gitlab.com/some/repo",
			expectedType:  &Gitlab{},
		},
		{
			name:          "returns github platform for github URLs",
			repositoryURL: "https://github.com/some/repo",
			expectedType:  &Github{},
		},
		{
			name:          "defaults to gitlab platform for unknown providers",
			repositoryURL: "https://gitfoo.com/some/repo",
			expectedType:  &Gitlab{},
		},
		{
			// A GitHub repo whose name contains "gitlab" must still route to GitHub:
			// detection is on the host, not a substring of the whole URL.
			name:          "routes github repo named gitlab-x to github",
			repositoryURL: "https://github.com/foo/gitlab-migration",
			expectedType:  &Github{},
		},
		{
			name:          "handles scp-style git URLs",
			repositoryURL: "git@github.com:owner/repo.git",
			expectedType:  &Github{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			factory := NewDefaultVcsProviderFactory()

			provider, err := factory.Create(tt.repositoryURL, "dummy-token", zap.NewNop())

			require.NoError(t, err)
			assert.IsType(t, tt.expectedType, provider)
		})
	}
}

func TestDefaultVcsProviderFactory_Create_CIEnvVars(t *testing.T) {
	tests := []struct {
		name          string
		repositoryURL string
		envKey        string
		envValue      string
		expectedType  any
	}{
		{
			name:          "GITHUB_ACTIONS overrides hostname detection for self-hosted GitHub",
			repositoryURL: "https://git.company.com/owner/repo",
			envKey:        "GITHUB_ACTIONS",
			envValue:      "true",
			expectedType:  &Github{},
		},
		{
			name:          "GITLAB_CI overrides hostname detection for self-hosted GitLab",
			repositoryURL: "https://git.company.com/group/repo",
			envKey:        "GITLAB_CI",
			envValue:      "true",
			expectedType:  &Gitlab{},
		},
		{
			name:          "GITHUB_ACTIONS wins over gitlab.com URL",
			repositoryURL: "https://gitlab.com/owner/repo",
			envKey:        "GITHUB_ACTIONS",
			envValue:      "true",
			expectedType:  &Github{},
		},
		{
			name:          "GITLAB_CI wins over github.com URL",
			repositoryURL: "https://github.com/owner/repo",
			envKey:        "GITLAB_CI",
			envValue:      "true",
			expectedType:  &Gitlab{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(tt.envKey, tt.envValue)

			factory := NewDefaultVcsProviderFactory()
			provider, err := factory.Create(tt.repositoryURL, "dummy-token", zap.NewNop())

			require.NoError(t, err)
			assert.IsType(t, tt.expectedType, provider)
		})
	}
}

func TestProviderFromEnv(t *testing.T) {
	tests := []struct {
		name     string
		env      map[string]string
		expected string
	}{
		{
			name:     "returns github when GITHUB_ACTIONS is true",
			env:      map[string]string{"GITHUB_ACTIONS": "true"},
			expected: "github",
		},
		{
			name:     "returns gitlab when GITLAB_CI is true",
			env:      map[string]string{"GITLAB_CI": "true"},
			expected: "gitlab",
		},
		{
			name:     "returns empty when no CI env vars are set",
			env:      map[string]string{},
			expected: "",
		},
		{
			name:     "GITHUB_ACTIONS=false does not trigger github detection",
			env:      map[string]string{"GITHUB_ACTIONS": "false"},
			expected: "",
		},
		{
			name:     "GITLAB_CI=false does not trigger gitlab detection",
			env:      map[string]string{"GITLAB_CI": "false"},
			expected: "",
		},
		{
			name:     "GITHUB_ACTIONS takes priority over GITLAB_CI",
			env:      map[string]string{"GITHUB_ACTIONS": "true", "GITLAB_CI": "true"},
			expected: "github",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("GITHUB_ACTIONS", "")
			t.Setenv("GITLAB_CI", "")
			for k, v := range tt.env {
				t.Setenv(k, v)
			}

			assert.Equal(t, tt.expected, providerFromEnv())
		})
	}
}

func TestDefaultVcsProviderFactory_Create_InvalidURL(t *testing.T) {
	factory := NewDefaultVcsProviderFactory()

	_, err := factory.Create("", "dummy-token", zap.NewNop())
	require.Error(t, err)
}

func TestParseGitURL(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		wantHost string
		wantPath string
		wantErr  bool
	}{
		{name: "https", raw: "https://gitlab.com/user/repo", wantHost: "gitlab.com", wantPath: "user/repo"},
		{name: "https with .git", raw: "https://gitlab.com/user/repo.git", wantHost: "gitlab.com", wantPath: "user/repo"},
		{name: "https with trailing slash", raw: "https://gitlab.com/user/repo/", wantHost: "gitlab.com", wantPath: "user/repo"},
		{name: "nested group", raw: "https://gitlab.com/group/user/repo.git", wantHost: "gitlab.com", wantPath: "group/user/repo"},
		{name: "scp style", raw: "git@github.com:owner/repo.git", wantHost: "github.com", wantPath: "owner/repo"},
		{name: "empty", raw: "", wantErr: true},
		{name: "no host", raw: "https:///user/repo", wantErr: true},
		{name: "scp without path", raw: "git@github.com", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host, path, err := parseGitURL(tt.raw)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantHost, host)
			assert.Equal(t, tt.wantPath, path)
		})
	}
}
