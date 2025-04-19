package services

import (
	"os"
	"testing"

	internal "github.com/drupdater/drupdater/internal"
	"github.com/drupdater/drupdater/internal/codehosting"
	"github.com/drupdater/drupdater/pkg/composer"
	"github.com/drupdater/drupdater/pkg/drupal"

	"github.com/stretchr/testify/assert"
	mock "github.com/stretchr/testify/mock"
	"go.uber.org/zap"
)

func TestGetFixedAdvisories(t *testing.T) {
	tests := []struct {
		name     string
		before   []composer.Advisory
		after    []composer.Advisory
		expected []composer.Advisory
	}{
		{
			name: "No advisories fixed",
			before: []composer.Advisory{
				{CVE: "CVE-1234", Title: "Vulnerability 1"},
				{CVE: "CVE-5678", Title: "Vulnerability 2"},
			},
			after: []composer.Advisory{
				{CVE: "CVE-1234", Title: "Vulnerability 1"},
				{CVE: "CVE-5678", Title: "Vulnerability 2"},
			},
			expected: []composer.Advisory{},
		},
		{
			name: "Some advisories fixed",
			before: []composer.Advisory{
				{CVE: "CVE-1234", Title: "Vulnerability 1"},
				{CVE: "CVE-5678", Title: "Vulnerability 2"},
			},
			after: []composer.Advisory{
				{CVE: "CVE-1234", Title: "Vulnerability 1"},
			},
			expected: []composer.Advisory{
				{CVE: "CVE-5678", Title: "Vulnerability 2"},
			},
		},
		{
			name: "All advisories fixed",
			before: []composer.Advisory{
				{CVE: "CVE-1234", Title: "Vulnerability 1"},
				{CVE: "CVE-5678", Title: "Vulnerability 2"},
			},
			after: []composer.Advisory{},
			expected: []composer.Advisory{
				{CVE: "CVE-1234", Title: "Vulnerability 1"},
				{CVE: "CVE-5678", Title: "Vulnerability 2"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ws := &SecurityUpdateStrategy{
				beforeAudit: composer.Audit{Advisories: tt.before},
				afterAudit:  composer.Audit{Advisories: tt.after},
			}
			actual := ws.GetFixedAdvisories()
			assert.Equal(t, tt.expected, actual)
		})
	}
}

func TestSecurityUpdateStartUpdate(t *testing.T) {
	logger := zap.NewNop()
	installer := drupal.NewMockInstallerService(t)
	updater := NewMockUpdaterService(t)
	repositoryService := NewMockRepositoryService(t)
	vcsProviderFactory := codehosting.NewMockVcsProviderFactory(t)
	vcsProvider := codehosting.NewMockPlatform(t)
	repository := internal.NewMockRepository(t)

	composerService := composer.NewMockRunner(t)
	config := internal.Config{
		RepositoryURL: "https://example.com/repo.git",
		Branch:        "main",
		Token:         "token",
		Sites:         []string{"site1", "site2"},
		DryRun:        false,
	}

	strategy := NewSecurityUpdateStrategy(logger, config, composerService)
	workflowService := NewWorkflowBaseService(logger, config, updater, vcsProviderFactory, repositoryService, installer, composerService)

	worktree := internal.NewMockWorktree(t)
	worktree.On("Checkout", mock.Anything).Return(nil)

	installer.On("InstallDrupal", mock.Anything, "/tmp", config.Sites).Return(nil)
	repositoryService.On("CloneRepository", config.RepositoryURL, config.Branch, config.Token).Return(repository, worktree, "/tmp", nil)
	repositoryService.On("BranchExists", mock.Anything, "security-update-ddd").Return(false, nil)

	updater.On("UpdateDependencies", mock.Anything, "/tmp", []string{"package1"}, mock.Anything, true).Return(DependencyUpdateReport{}, nil)
	updater.On("UpdateDrupal", mock.Anything, "/tmp", mock.Anything, config.Sites).Return(UpdateHooksPerSite{}, nil)
	vcsProviderFactory.On("Create", "https://example.com/repo.git", "token").Return(vcsProvider)

	fixture, _ := os.ReadFile("testdata/security_update.md")
	vcsProvider.On("CreateMergeRequest", mock.Anything, string(fixture), mock.Anything, config.Branch).Return(codehosting.MergeRequest{}, nil)
	repository.On("Push", mock.Anything).Return(nil)
	composerService.On("Diff", mock.Anything, mock.Anything, mock.Anything, true).Return("Dummy Table", nil)
	composerService.On("Audit", mock.Anything, "/tmp").Return(composer.Audit{
		Advisories: []composer.Advisory{
			{CVE: "CVE-1234", Title: "Vul 1", Severity: "high    ", Link: "https://example.com", PackageName: "package1"},
			{CVE: "CVE-5678", Title: "Vul 2", Severity: "high    ", Link: "https://example.com", PackageName: "package1"},
		},
	}, nil)
	composerService.On("GetLockHash", "/tmp").Return("ddd", nil)
	composerService.On("Install", mock.Anything, "/tmp").Return(nil)

	err := workflowService.StartUpdate(t.Context(), strategy)

	assert.NoError(t, err)
	installer.AssertExpectations(t)
	repositoryService.AssertExpectations(t)
	updater.AssertExpectations(t)
	vcsProviderFactory.AssertExpectations(t)
	vcsProvider.AssertExpectations(t)
}

func TestSecurityUpdateStartUpdateWithDryRun(t *testing.T) {
	logger := zap.NewNop()
	installer := drupal.NewMockInstallerService(t)
	updater := NewMockUpdaterService(t)
	repositoryService := NewMockRepositoryService(t)
	vcsProviderFactory := codehosting.NewMockVcsProviderFactory(t)
	vcsProvider := codehosting.NewMockPlatform(t)
	repository := internal.NewMockRepository(t)

	composerService := composer.NewMockRunner(t)
	config := internal.Config{
		RepositoryURL: "https://example.com/repo.git",
		Branch:        "main",
		Token:         "token",
		Sites:         []string{"site1", "site2"},
		DryRun:        true,
	}
	strategy := NewSecurityUpdateStrategy(logger, config, composerService)
	workflowService := NewWorkflowBaseService(logger, config, updater, vcsProviderFactory, repositoryService, installer, composerService)

	worktree := internal.NewMockWorktree(t)
	worktree.On("Checkout", mock.Anything).Return(nil)
	installer.On("InstallDrupal", mock.Anything, "/tmp", config.Sites).Return(nil)
	repositoryService.On("BranchExists", mock.Anything, "security-update-ddd").Return(false, nil)
	repositoryService.On("CloneRepository", config.RepositoryURL, config.Branch, config.Token).Return(repository, worktree, "/tmp", nil)
	updater.On("UpdateDependencies", mock.Anything, "/tmp", []string{"package1"}, mock.Anything, true).Return(DependencyUpdateReport{}, nil)
	updater.On("UpdateDrupal", mock.Anything, "/tmp", mock.Anything, config.Sites).Return(UpdateHooksPerSite{}, nil)
	composerService.On("Diff", mock.Anything, mock.Anything, mock.Anything, true).Return("foo", nil)
	composerService.On("Audit", mock.Anything, "/tmp").Return(composer.Audit{
		Advisories: []composer.Advisory{
			{CVE: "CVE-1234", Title: "Vul 1", Severity: "high    ", Link: "https://example.com", PackageName: "package1"},
			{CVE: "CVE-5678", Title: "Vul 2", Severity: "high    ", Link: "https://example.com", PackageName: "package1"},
		},
	}, nil)
	composerService.On("GetLockHash", "/tmp").Return("ddd", nil)
	composerService.On("Install", mock.Anything, "/tmp").Return(nil)

	err := workflowService.StartUpdate(t.Context(), strategy)

	assert.NoError(t, err)
	installer.AssertExpectations(t)
	repositoryService.AssertExpectations(t)
	updater.AssertExpectations(t)
	vcsProviderFactory.AssertExpectations(t)
	vcsProvider.AssertExpectations(t)
}
