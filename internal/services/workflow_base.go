package services

import (
	"bytes"
	"context"
	"crypto/md5"
	"embed"
	"fmt"
	"os"
	"sync"
	"text/template"

	"github.com/drupdater/drupdater/internal"
	"github.com/drupdater/drupdater/internal/codehosting"
	"github.com/drupdater/drupdater/pkg/composer"

	git "github.com/go-git/go-git/v5"
	gitConfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"go.uber.org/zap"
)

//go:embed templates
var templates embed.FS

type TemplateData struct {
	ComposerDiff           string
	DependencyUpdateReport DependencyUpdateReport
	SecurityReport         SecurityReport
	UpdateHooks            UpdateHooksPerSite
}

type SecurityReport struct {
	FixedAdvisories       []composer.Advisory
	AfterUpdateAdvisories []composer.Advisory
	NumUnresolvedIssues   int
}

type WorkflowService interface {
	StartUpdate(ctx context.Context, strategy WorkflowStrategy) error
}

type WorkflowUpdateResult struct {
	updateReport DependencyUpdateReport
	table        string
}

type WorkflowBaseService struct {
	logger             *zap.Logger
	config             internal.Config
	updater            UpdaterService
	vcsProviderFactory codehosting.VcsProviderFactory
	repository         RepositoryService
	installer          InstallerService
	composer           composer.Runner
}

func NewWorkflowBaseService(
	logger *zap.Logger,
	config internal.Config,
	updater UpdaterService,
	vcsProviderFactory codehosting.VcsProviderFactory,
	repository RepositoryService,
	installer InstallerService,
	composerService composer.Runner,
) *WorkflowBaseService {
	return &WorkflowBaseService{
		logger:             logger,
		config:             config,
		updater:            updater,
		vcsProviderFactory: vcsProviderFactory,
		repository:         repository,
		installer:          installer,
		composer:           composerService,
	}
}

func (ws *WorkflowBaseService) StartUpdate(ctx context.Context, strategy WorkflowStrategy) error {
	ws.logger.Info("starting update workflow")

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	errCh := make(chan error, 2)
	resultCh := make(chan WorkflowUpdateResult, 1)

	wg.Add(2)

	go func() {
		defer wg.Done()
		if err := ws.installer.InstallDrupal(ctx, ws.config.RepositoryURL, ws.config.Branch, ws.config.Token, ws.config.Sites); err != nil {
			errCh <- err
			cancel()
		}
	}()

	ws.logger.Info("cloning repository for update", zap.String("repositoryURL", ws.config.RepositoryURL), zap.String("branch", ws.config.Branch))
	repository, worktree, path, err := ws.repository.CloneRepository(ws.config.RepositoryURL, ws.config.Branch, ws.config.Token)
	if err != nil {
		return err
	}

	// Use strategy to determine what to update
	packagesToUpdate, minimalChanges, err := strategy.PreUpdate(ctx, path)
	if err != nil {
		return err
	}

	// Check if we should continue with the update
	if !strategy.ShouldContinue(packagesToUpdate) {
		return nil
	}

	go func() {
		defer wg.Done()

		ws.logger.Info("updating dependencies")
		updateReport, err := ws.updater.UpdateDependencies(ctx, path, packagesToUpdate, worktree, minimalChanges)
		if err != nil {
			errCh <- err
			return
		}

		table, err := ws.composer.Diff(ctx, path, ws.config.Branch, true)
		if err != nil {
			errCh <- err
			return
		}

		if table == "" {
			ws.logger.Info("no packages were updated, skipping update")
			errCh <- err
			return
		}

		ws.logger.Sugar().Info("composer diff table", fmt.Sprintf("\n%s", table))

		resultCh <- WorkflowUpdateResult{
			updateReport: updateReport,
			table:        table,
		}
	}()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	var result WorkflowUpdateResult

	select {
	case <-done:
		// Both finished, now fetch result
		select {
		case result = <-resultCh:
		default:
		}
	case err := <-errCh:
		ws.logger.Sugar().Error(err)
		return err
	}

	// Get branch name from strategy
	updateBranchName := strategy.GenerateBranchName(path)

	// Check if branch already exists
	exists, err := ws.repository.BranchExists(repository, updateBranchName)
	if err != nil {
		ws.logger.Error("failed to check if branch exists", zap.Error(err))
	}
	if exists {
		ws.logger.Info("branch already exists", zap.String("branch", updateBranchName))
		return nil
	}

	// Create final branch for changes
	if err := worktree.Checkout(&git.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName(updateBranchName),
		Create: true,
		Force:  false,
		Keep:   true,
	}); err != nil {
		ws.logger.Error("failed to checkout branch", zap.Error(err))
		return err
	}

	// Run post-update actions from strategy
	if err := strategy.PostUpdate(ctx, path, worktree); err != nil {
		return err
	}

	// Update Drupal hooks
	updateHooks, err := ws.updater.UpdateDrupal(ctx, path, worktree, ws.config.Sites)
	if err != nil {
		return err
	}

	if !ws.config.DryRun {
		if err = ws.PushChanges(repository, updateBranchName); err != nil {
			return err
		}

		// Use strategy to get PR details
		title, templateName := strategy.GeneratePRDetails()

		// Get template data from strategy
		data, err := strategy.GetTemplateData(result, updateHooks)
		if err != nil {
			ws.logger.Error("failed to get template data", zap.Error(err))
			return err
		}

		// Generate description and create MR
		description, err := ws.GenerateDescription(data, templateName)
		if err != nil {
			ws.logger.Error("failed to generate description", zap.Error(err))
			return err
		}

		if _, err := ws.CreateMergeRequest(title, description, updateBranchName, ws.config.Branch); err != nil {
			ws.logger.Error("failed to create merge request", zap.Error(err))
			return err
		}
	}

	// Clean up the temporary directory
	defer ws.Cleanup()

	return nil
}

func (ws *WorkflowBaseService) PushChanges(repository internal.Repository, updateBranchName string) error {
	err := repository.Push(&git.PushOptions{
		RemoteName: "origin",
		RefSpecs: []gitConfig.RefSpec{
			gitConfig.RefSpec(fmt.Sprintf("refs/heads/%s:refs/heads/%s", updateBranchName, updateBranchName)),
		},
		Auth: &http.BasicAuth{
			Username: "du", // yes, this can be anything except an empty string
			Password: ws.config.Token,
		},
	})

	if err != nil {
		ws.logger.Error("failed to push changes", zap.Error(err))
		return err
	}

	return nil
}

func (ws *WorkflowBaseService) GenerateDescription(data interface{}, filename string) (string, error) {
	tmpl, err := template.ParseFS(templates, "templates/*.go.tmpl")
	if err != nil {
		panic(err)
	}

	var output bytes.Buffer

	err = tmpl.ExecuteTemplate(&output, filename, data)
	if err != nil {
		return "", err
	}

	return output.String(), nil
}

func (ws *WorkflowBaseService) CreateMergeRequest(title string, description string, updateBranchName string, baseBranch string) (codehosting.MergeRequest, error) {
	codehostingPlatform := ws.vcsProviderFactory.Create(ws.config.RepositoryURL, ws.config.Token)
	mr, err := codehostingPlatform.CreateMergeRequest(title, description, updateBranchName, baseBranch)
	if err != nil {
		ws.logger.Error("failed to create merge request", zap.Error(err))
		return codehosting.MergeRequest{}, err
	}
	ws.logger.Info("merge request created", zap.String("url", mr.URL))
	return mr, nil
}

func (ws *WorkflowBaseService) Cleanup() {
	tmpDirName := fmt.Sprintf("/tmp/%x", md5.Sum([]byte(ws.config.RepositoryURL)))
	os.RemoveAll(tmpDirName)
}
