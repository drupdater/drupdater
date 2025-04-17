package services

import (
	"context"
	"fmt"
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
	afterUpdate []AfterUpdate
	installer   InstallerService
	repository  RepositoryService
}

func newWorkflowDependencyUpdateService(afterUpdate []AfterUpdate, logger *zap.Logger, installer InstallerService, updater UpdaterService, repository RepositoryService, vcsProviderFactory codehosting.VcsProviderFactory, config internal.Config, commandExecutor utils.CommandExecutor) *WorkflowDependencyUpdateService {
	return &WorkflowDependencyUpdateService{
		WorkflowBaseService: WorkflowBaseService{
			logger:             logger,
			config:             config,
			updater:            updater,
			commandExecutor:    commandExecutor,
			vcsProviderFactory: vcsProviderFactory,
		},
		afterUpdate: afterUpdate,
		installer:   installer,
		repository:  repository,
	}
}

func (ws *WorkflowDependencyUpdateService) StartUpdate(ctx context.Context) error {
	ws.logger.Info("starting update workflow")

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	errCh := make(chan error, 2)
	resultCh := make(chan WorkflowUpdateResult, 1) // channel for result from one of the goroutines

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

	if err := worktree.Checkout(&git.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName(updateBranchName),
		Create: true,
	}); err != nil {
		ws.logger.Error("failed to checkout branch", zap.Error(err))
		return err
	}

	go ws.Update(errCh, resultCh, ctx, []string{}, false, path, worktree, &wg)

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

	if !ws.config.DryRun {
		if err = ws.PushChanges(repository, updateBranchName); err != nil {
			return err
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

		title := fmt.Sprintf("%s: Drupal Maintenance Updates", time.Now().Format("January 2006"))
		ws.CreateMergeRequest(title, description, updateBranchName, ws.config.Branch)
	}

	// Clean up the temporary directory
	defer ws.Cleanup()

	return nil
}
