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
