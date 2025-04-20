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
	"github.com/drupdater/drupdater/pkg/drupal"
	"github.com/drupdater/drupdater/pkg/drush"

	git "github.com/go-git/go-git/v5"
	gitConfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
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
	ws.logger.Info("starting update workflow")

	// Clean up the temporary directory
	defer ws.Cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	errCh := make(chan error, len(ws.config.Sites)*2+2) // site installs + site updates + installCode + updateCode

	// Channels to coordinate
	installCodeDone := make(chan string, 1) // carries installPath
	installSiteDone := make(map[string]chan struct{})

	for _, site := range ws.config.Sites {
		installSiteDone[site] = make(chan struct{})
	}

	var sharedUpdate SharedUpdate
	var updateDone = make(chan struct{})
	var siteReports sync.Map // map[string]UpdateReport

	// 1. Run installCode()
	wg.Add(1)
	go func() {
		defer wg.Done()

		path, err := ws.installCode(ctx)
		if err != nil {
			errCh <- err
			cancel()
			return
		}

		// Broadcast path to all waiting goroutines
		installPath := path
		installCodeDone <- installPath
	}()

	// 2. Run installSite(site) after installCode
	for _, site := range ws.config.Sites {
		wg.Add(1)
		go func(site string) {
			defer wg.Done()

			select {
			case installPath := <-installCodeDone:
				// Put the path back for other goroutines
				installCodeDone <- installPath

				if err := ws.installer.InstallDrupal(ctx, installPath, site); err != nil {
					errCh <- err
					cancel()
					return
				}
				close(installSiteDone[site])
			case <-ctx.Done():
				return
			}
		}(site)
	}

	// 3. Run updateSharedCode() in parallel
	wg.Add(1)
	go func() {
		defer wg.Done()
		var err error
		update, err := ws.updateSharedCode(ctx, strategy)
		if err != nil {
			errCh <- err
			cancel()
			return
		}

		// Simply assign the update to sharedUpdate - no need for mutex
		// since all readers will wait for updateDone channel before accessing
		sharedUpdate = update
		close(updateDone)
	}()

	// 4. Run updateSite(site) after installSite + updateSharedCode
	for _, site := range ws.config.Sites {
		wg.Add(1)
		go func(site string) {
			defer wg.Done()

			// Wait for install to finish
			select {
			case <-installSiteDone[site]:
			case <-ctx.Done():
				return
			}

			// Wait for shared update to be ready
			select {
			case <-updateDone:
			case <-ctx.Done():
				return
			}

			// Update Drupal hooks
			updateHooks, err := ws.updater.UpdateDrupal(ctx, sharedUpdate.Path, sharedUpdate.Worktree, site)
			if err != nil {
				errCh <- err
				cancel()
				return
			}

			siteReports.Store(site, updateHooks)

		}(site)
	}

	// 5. Wait for all routines
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:

		if !ws.config.DryRun {

			finalReports := make(UpdateHooksPerSite)
			siteReports.Range(func(key, value any) bool {
				site := key.(string)
				report := value.(map[string]drush.UpdateHook)
				finalReports[site] = report
				return true
			})

			ws.publishWork(sharedUpdate.Repository, sharedUpdate.updateBranchName, strategy, sharedUpdate.WorkflowUpdateResult, finalReports)
		}
		return nil
	case err := <-errCh:
		return err
	}
}

func (ws *WorkflowBaseService) installCode(ctx context.Context) (string, error) {
	ws.logger.Info("cloning repository for site-install", zap.String("repositoryURL", ws.config.RepositoryURL), zap.String("branch", ws.config.Branch))
	_, _, path, err := ws.repository.CloneRepository(ws.config.RepositoryURL, ws.config.Branch, ws.config.Token)
	if err != nil {
		ws.logger.Error("failed to clone repository", zap.String("repositoryURL", ws.config.RepositoryURL), zap.String("branch", ws.config.Branch), zap.Error(err))
		return "", err
	}

	if err = ws.composer.Install(ctx, path); err != nil {
		return "", err
	}

	return path, nil
}

func (ws *WorkflowBaseService) updateSharedCode(ctx context.Context, strategy WorkflowStrategy) (SharedUpdate, error) {
	ws.logger.Info("cloning repository for update", zap.String("repositoryURL", ws.config.RepositoryURL), zap.String("branch", ws.config.Branch))
	repository, worktree, path, err := ws.repository.CloneRepository(ws.config.RepositoryURL, ws.config.Branch, ws.config.Token)
	if err != nil {
		return SharedUpdate{}, err
	}

	// Use strategy to determine what to update
	packagesToUpdate, minimalChanges, err := strategy.PreUpdate(ctx, path)
	if err != nil {

		return SharedUpdate{}, err
	}

	// Check if we should continue with the update
	if !strategy.ShouldContinue(packagesToUpdate) {

		return SharedUpdate{}, err
	}

	ws.logger.Info("updating dependencies")
	updateReport, err := ws.updater.UpdateDependencies(ctx, path, packagesToUpdate, worktree, minimalChanges)
	if err != nil {

		return SharedUpdate{}, err
	}

	table, err := ws.composer.Diff(ctx, path, ws.config.Branch, true)
	if err != nil {

		return SharedUpdate{}, err
	}

	if table == "" {
		ws.logger.Info("no packages were updated, skipping update")

		return SharedUpdate{}, err
	}

	ws.logger.Sugar().Info("composer diff table", fmt.Sprintf("\n%s", table))

	updateBranchName := strategy.GenerateBranchName(path)

	// Check if branch already exists
	exists, err := ws.repository.BranchExists(repository, updateBranchName)
	if err != nil {
		ws.logger.Error("failed to check if branch exists", zap.Error(err))
	}
	if exists {
		ws.logger.Info("branch already exists", zap.String("branch", updateBranchName))

		return SharedUpdate{}, err
	}

	// Create final branch for changes
	if err := worktree.Checkout(&git.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName(updateBranchName),
		Create: true,
		Force:  false,
		Keep:   true,
	}); err != nil {
		ws.logger.Error("failed to checkout branch", zap.Error(err))

		return SharedUpdate{}, err
	}

	// Run post-update actions from strategy
	if err := strategy.PostUpdate(ctx, path, worktree); err != nil {

		return SharedUpdate{}, err
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
		ws.logger.Error("failed to push changes", zap.Error(err))
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

	codehostingPlatform := ws.vcsProviderFactory.Create(ws.config.RepositoryURL, ws.config.Token)
	mr, err := codehostingPlatform.CreateMergeRequest(title, description, updateBranchName, ws.config.Branch)
	if err != nil {
		ws.logger.Error("failed to create merge request", zap.Error(err))
		return err
	}
	ws.logger.Info("merge request created", zap.String("url", mr.URL))

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

func (ws *WorkflowBaseService) Cleanup() {
	tmpDirName := fmt.Sprintf("/tmp/%x", md5.Sum([]byte(ws.config.RepositoryURL)))
	os.RemoveAll(tmpDirName)
}
