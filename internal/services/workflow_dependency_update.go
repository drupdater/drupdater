package services

import (
	"context"
	"crypto/md5"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/drupdater/drupdater/internal"
	"github.com/drupdater/drupdater/internal/codehosting"
	"github.com/drupdater/drupdater/internal/utils"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"go.uber.org/zap"
)

type WorkflowDependencyUpdateService struct {
	WorkflowBaseService
	afterUpdate        []AfterUpdate
	installer          InstallerService
	updater            UpdaterService
	repository         RepositoryService
	vcsProviderFactory codehosting.VcsProviderFactory
	commandExecutor    utils.CommandExecutor
}

func newWorkflowDependencyUpdateService(afterUpdate []AfterUpdate, logger *zap.Logger, installer InstallerService, updater UpdaterService, repository RepositoryService, vcsProviderFactory codehosting.VcsProviderFactory, config internal.Config, commandExecutor utils.CommandExecutor) *WorkflowDependencyUpdateService {
	return &WorkflowDependencyUpdateService{
		WorkflowBaseService: WorkflowBaseService{
			logger: logger,
			config: config,
		},
		afterUpdate:        afterUpdate,
		installer:          installer,
		updater:            updater,
		repository:         repository,
		vcsProviderFactory: vcsProviderFactory,
		commandExecutor:    commandExecutor,
	}
}

func (ws *WorkflowDependencyUpdateService) StartUpdate(ctx context.Context) error {
	ws.logger.Info("starting update workflow")

	type Result struct {
		updateReport DependencyUpdateReport
		table        string
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	errCh := make(chan error, 2)
	resultCh := make(chan Result, 1) // channel for result from one of the goroutines

	wg.Add(2)

	go func() {
		defer wg.Done()
		if err := ws.installer.InstallDrupal(ctx, ws.config.RepositoryURL, ws.config.Branch, ws.config.Token, ws.config.Sites); err != nil {
			errCh <- err
			cancel()
		}
	}()

	updateBranchName := fmt.Sprintf("update-%s", time.Now().Format("20060102150405"))
	ws.logger.Info("cloning repository for update", zap.String("repositoryURL", ws.config.RepositoryURL), zap.String("branch", ws.config.Branch))
	repository, worktree, path, err := ws.repository.CloneRepository(ws.config.RepositoryURL, ws.config.Branch, ws.config.Token)
	if err != nil {
		return err
	}

	go func() {
		defer wg.Done()

		if err := worktree.Checkout(&git.CheckoutOptions{
			Branch: plumbing.NewBranchReferenceName(updateBranchName),
			Create: true,
		}); err != nil {
			ws.logger.Error("failed to checkout branch", zap.Error(err))
			errCh <- err
			return
		}

		ws.logger.Info("updating dependencies")
		updateReport, err := ws.updater.UpdateDependencies(ctx, path, []string{}, worktree, false)
		if err != nil {
			errCh <- err
			return
		}

		table, err := ws.commandExecutor.GenerateDiffTable(ctx, path, ws.config.Branch, true)
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

		resultCh <- Result{
			updateReport: updateReport,
			table:        table,
		}
	}()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	var result Result

	select {
	case <-done:
		// Both finished, now fetch result
		select {
		case result = <-resultCh:
		default:
		}
	case err := <-errCh:
		ws.logger.Sugar().Error(err)
		// Optional: exit early or handle further
		return err
	}

	updateHooks, err := ws.updater.UpdateDrupal(ctx, path, worktree, ws.config.Sites)
	if err != nil {
		return err
	}

	for _, au := range ws.afterUpdate {
		if err := au.Execute(ctx, path, worktree); err != nil {
			ws.logger.Error("failed to execute after update", zap.Error(err))
			return err
		}
	}

	data := TemplateData{
		ComposerDiff:           result.table,
		DependencyUpdateReport: result.updateReport,
		UpdateHooks:            updateHooks,
	}

	description, err := ws.GenerateDescription(data, "dependency_update.go.tmpl")
	if err != nil {
		ws.logger.Error("failed to generate description", zap.Error(err))
		return err
	}

	if !ws.config.DryRun {
		if err = ws.PushChanges(repository, updateBranchName); err != nil {
			return err
		}

		ws.logger.Debug("creating merge request", zap.String("title", "Drupal Maintenance Updates"), zap.String("description", description), zap.String("sourceBranch", updateBranchName), zap.String("targetBranch", ws.config.Branch))
		codehostingPlatform := ws.vcsProviderFactory.Create(ws.config.RepositoryURL, ws.config.Token)
		title := fmt.Sprintf("%s: Drupal Maintenance Updates", time.Now().Format("January 2006"))
		mr, err := codehostingPlatform.CreateMergeRequest(title, description, updateBranchName, ws.config.Branch)
		if err != nil {
			ws.logger.Error("failed to create merge request", zap.Error(err))
			// remove the branch if the merge request creation failed
			//worktree.
		}
		ws.logger.Info("merge request created", zap.String("url", mr.URL))
	}

	// Clean up the temporary directory
	defer func() {
		tmpDirName := fmt.Sprintf("/tmp/%x", md5.Sum([]byte(ws.config.RepositoryURL)))
		os.RemoveAll(tmpDirName)
	}()

	return nil
}
