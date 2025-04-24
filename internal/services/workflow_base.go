package services

import (
	"bytes"
	"context"
	"crypto/md5"
	"embed"
	"fmt"
	"os"
	"text/template"

	"github.com/drupdater/drupdater/internal"
	"github.com/drupdater/drupdater/internal/codehosting"
	"github.com/drupdater/drupdater/pkg/composer"
	"github.com/drupdater/drupdater/pkg/drupal"
	"github.com/drupdater/drupdater/pkg/drush"

	git "github.com/go-git/go-git/v5"
	gitConfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	goflow "github.com/kamildrazkiewicz/go-flow"
	"go.uber.org/zap"
)

//go:embed templates
var templates embed.FS

type WorkflowService interface {
	StartUpdate(ctx context.Context, strategy WorkflowStrategy) error
}

type WorkflowUpdateResult struct {
	updateReport DependencyUpdateReport
	table        string
}

type SharedUpdate struct {
	Path                 string
	Worktree             internal.Worktree
	Repository           internal.Repository
	WorkflowUpdateResult WorkflowUpdateResult
	updateBranchName     string
}

type WorkflowBaseService struct {
	logger             *zap.Logger
	config             internal.Config
	updater            UpdaterService
	vcsProviderFactory codehosting.VcsProviderFactory
	repository         RepositoryService
	installer          drupal.InstallerService
	composer           composer.Runner
}

func NewWorkflowBaseService(
	logger *zap.Logger,
	config internal.Config,
	updater UpdaterService,
	vcsProviderFactory codehosting.VcsProviderFactory,
	repository RepositoryService,
	installer drupal.InstallerService,
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
	ctx, cancel := context.WithCancel(context.Background())

	defer func() {
		// Clean up the temporary directory
		tmpDirName := fmt.Sprintf("/tmp/%x", md5.Sum([]byte(ws.config.RepositoryURL)))
		os.RemoveAll(tmpDirName)

		cancel()
	}()

	installCode := func(r map[string]interface{}) (interface{}, error) {
		fmt.Println("function1 started")
		return ws.installCode(ctx)
	}

	updateSharedCode := func(r map[string]interface{}) (interface{}, error) {
		fmt.Println("function2 started")
		return ws.updateSharedCode(ctx, strategy)
	}

	flow := goflow.New().
		Add("installCode", nil, installCode).
		Add("updateSharedCode", nil, updateSharedCode)

	for _, site := range ws.config.Sites {
		installSite := func(r map[string]interface{}) (interface{}, error) {
			fmt.Println("function2 started")
			installPath := r["installCode"].(string)
			return nil, ws.installer.InstallDrupal(ctx, installPath, site)
		}
		flow.Add("installSite"+site, []string{"installCode"}, installSite)

		updateSite := func(r map[string]interface{}) (interface{}, error) {
			fmt.Println("function3 started")
			sharedUpdate := r["updateSharedCode"].(SharedUpdate)
			return ws.updater.UpdateDrupal(ctx, sharedUpdate.Path, sharedUpdate.Worktree, site)
		}
		flow.Add("updateSite"+site, []string{"installSite" + site, "updateSharedCode"}, updateSite)
	}

	result, err := flow.Do()
	if err != nil {
		cancel()
		return fmt.Errorf("failed to execute flow: %w", err)
	}

	// Get the result of the last function
	sharedUpdate := result["updateSharedCode"].(SharedUpdate)
	updateHooks := UpdateHooksPerSite{}
	for _, site := range ws.config.Sites {
		updateHooks[site] = result["updateSite"+site].(map[string]drush.UpdateHook)
	}

	if !ws.config.DryRun {
		if err := ws.publishWork(sharedUpdate.Repository, sharedUpdate.updateBranchName, strategy, sharedUpdate.WorkflowUpdateResult, updateHooks); err != nil {
			cancel()
			return fmt.Errorf("failed to publish work: %w", err)
		}
	}
	return nil
}

func (ws *WorkflowBaseService) installCode(ctx context.Context) (string, error) {
	ws.logger.Info("cloning repository for site-install", zap.String("repositoryURL", ws.config.RepositoryURL), zap.String("branch", ws.config.Branch))
	_, _, path, err := ws.repository.CloneRepository(ws.config.RepositoryURL, ws.config.Branch, ws.config.Token)
	if err != nil {
		return "", fmt.Errorf("failed to clone repository: %w", err)
	}

	ws.logger.Info("running composer install")
	if err = ws.composer.Install(ctx, path); err != nil {
		return "", fmt.Errorf("failed to run composer install: %w", err)
	}

	return path, nil
}

func (ws *WorkflowBaseService) updateSharedCode(ctx context.Context, strategy WorkflowStrategy) (SharedUpdate, error) {
	ws.logger.Info("cloning repository for update", zap.String("repositoryURL", ws.config.RepositoryURL), zap.String("branch", ws.config.Branch))
	repository, worktree, path, err := ws.repository.CloneRepository(ws.config.RepositoryURL, ws.config.Branch, ws.config.Token)
	if err != nil {
		return SharedUpdate{}, fmt.Errorf("failed to clone repository: %w", err)
	}

	// Use strategy to determine what to update
	packagesToUpdate, minimalChanges, err := strategy.PreUpdate(ctx, path)
	if err != nil {

		return SharedUpdate{}, fmt.Errorf("failed to pre-update: %w", err)
	}

	// Check if we should continue with the update
	if !strategy.ShouldContinue(packagesToUpdate) {
		return SharedUpdate{}, fmt.Errorf("update skipped by strategy")
	}

	ws.logger.Info("updating dependencies")
	updateReport, err := ws.updater.UpdateDependencies(ctx, path, packagesToUpdate, worktree, minimalChanges)
	if err != nil {
		return SharedUpdate{}, fmt.Errorf("failed to update dependencies: %w", err)
	}

	table, err := ws.composer.Diff(ctx, path, ws.config.Branch, true)
	if err != nil {
		return SharedUpdate{}, fmt.Errorf("failed to get diff: %w", err)
	}

	if table == "" {
		return SharedUpdate{}, fmt.Errorf("no packages were updated, skipping update")
	}

	ws.logger.Sugar().Info("composer diff table", fmt.Sprintf("\n%s", table))

	updateBranchName := strategy.GenerateBranchName(path)

	// Check if branch already exists
	exists, err := ws.repository.BranchExists(repository, updateBranchName)
	if err != nil {
		return SharedUpdate{}, fmt.Errorf("failed to check if branch exists: %w", err)
	}
	if exists {
		return SharedUpdate{}, fmt.Errorf("branch %s already exists, skipping", updateBranchName)
	}

	// Create final branch for changes
	if err := worktree.Checkout(&git.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName(updateBranchName),
		Create: true,
		Force:  false,
		Keep:   true,
	}); err != nil {
		return SharedUpdate{}, fmt.Errorf("failed to checkout branch: %w", err)
	}

	// Run post-update actions from strategy
	if err := strategy.PostUpdate(ctx, path, worktree); err != nil {
		return SharedUpdate{}, fmt.Errorf("failed to post-update: %w", err)
	}

	sharedUpdate := SharedUpdate{
		Path:                 path,
		Worktree:             worktree,
		WorkflowUpdateResult: WorkflowUpdateResult{updateReport: updateReport, table: table},
		Repository:           repository,
		updateBranchName:     updateBranchName,
	}
	return sharedUpdate, nil
}

func (ws *WorkflowBaseService) publishWork(repository internal.Repository, updateBranchName string, strategy WorkflowStrategy, result WorkflowUpdateResult, updateHooks UpdateHooksPerSite) error {
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
		return fmt.Errorf("failed to push changes: %w", err)
	}

	// Use strategy to get PR details
	title, templateName := strategy.GeneratePRDetails()

	// Get template data from strategy
	data, err := strategy.GetTemplateData(result, updateHooks)
	if err != nil {
		return fmt.Errorf("failed to get template data: %w", err)
	}

	// Generate description and create MR
	description, err := ws.GenerateDescription(data, templateName)
	if err != nil {
		return fmt.Errorf("failed to generate description: %w", err)
	}

	codehostingPlatform := ws.vcsProviderFactory.Create(ws.config.RepositoryURL, ws.config.Token)
	mr, err := codehostingPlatform.CreateMergeRequest(title, description, updateBranchName, ws.config.Branch)
	if err != nil {
		return fmt.Errorf("failed to create merge request: %w", err)
	}
	ws.logger.Info("merge request created", zap.String("url", mr.URL))

	return nil
}

func (ws *WorkflowBaseService) GenerateDescription(data interface{}, filename string) (string, error) {
	tmpl, err := template.ParseFS(templates, "templates/*.go.tmpl")
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %w", err)
	}

	var output bytes.Buffer

	err = tmpl.ExecuteTemplate(&output, filename, data)
	if err != nil {
		return "", fmt.Errorf("failed to execute template: %w", err)
	}

	return output.String(), nil
}
