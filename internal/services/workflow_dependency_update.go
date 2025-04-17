package services

import (
	"context"
	"crypto/md5"
	"fmt"
	"os"
	"sync"
	"time"
	"unicode/utf8"

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
	var wg sync.WaitGroup
	wg.Add(2)

	type Done struct {
		updateReport     DependencyUpdateReport
		path             string
		worktree         internal.Worktree
		table            string
		repository       internal.Repository
		updateBranchName string
	}

	errChannel := make(chan error, 2)
	doneChannel := make(chan Done)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		defer wg.Done()
		err := ws.installer.InstallDrupal(ctx, ws.config.RepositoryURL, ws.config.Branch, ws.config.Token, ws.config.Sites)
		if err != nil {
			errChannel <- err
		}
	}()

	go func() {
		defer wg.Done()

		updateBranchName := fmt.Sprintf("update-%s", time.Now().Format("20060102150405"))
		ws.logger.Info("cloning repository for update", zap.String("repositoryURL", ws.config.RepositoryURL), zap.String("branch", ws.config.Branch))
		repository, worktree, path, err := ws.repository.CloneRepository(ws.config.RepositoryURL, ws.config.Branch, ws.config.Token)
		if err != nil {
			errChannel <- err
			return
		}

		if err := worktree.Checkout(&git.CheckoutOptions{
			Branch: plumbing.NewBranchReferenceName(updateBranchName),
			Create: true,
		}); err != nil {
			ws.logger.Error("failed to checkout branch", zap.Error(err))
			errChannel <- err
			return
		}

		beforeUpdateCommit, _ := ws.repository.GetHeadCommit(repository)
		ws.logger.Info("updating dependencies")
		updateReport, err := ws.updater.UpdateDependencies(ctx, path, []string{}, worktree, false)
		if err != nil {
			errChannel <- err
			return
		}

		table, err := ws.commandExecutor.GenerateDiffTable(ctx, path, beforeUpdateCommit.Hash.String(), true)
		if err != nil {
			errChannel <- err
			return
		}

		if table == "" {
			ws.logger.Info("no packages were updated, skipping update")
			errChannel <- err
			return
		}

		tableForLog, err := ws.commandExecutor.GenerateDiffTable(ctx, path, beforeUpdateCommit.Hash.String(), false)
		if err != nil {
			errChannel <- err
			return
		}
		ws.logger.Sugar().Info("composer diff table", fmt.Sprintf("\n%s", tableForLog))

		// If table is too long, Github/Gitlab will not accept it. So we use the version without the links.
		tableCharCount := utf8.RuneCountInString(table)
		if tableCharCount > 63000 {
			table = tableForLog
		}

		doneChannel <- Done{
			updateReport:     updateReport,
			table:            table,
			path:             path,
			worktree:         worktree,
			repository:       repository,
			updateBranchName: updateBranchName,
		}
	}()

	doneHelp := make(chan struct{})
	go func() {
		wg.Wait()
		close(doneHelp)
	}()

	help := Done{}

	select {
	case done := <-doneChannel:
		help = done
		// All goroutines finished successfully
	case err := <-errChannel:
		// An error occurred in one of the goroutines
		ws.logger.Sugar().Error(err)
		cancel()
	}

	updateHooks, err := ws.updater.UpdateDrupal(ctx, help.path, help.worktree, ws.config.Sites)
	if err != nil {
		return err
	}

	for _, au := range ws.afterUpdate {
		if err := au.Execute(ctx, help.path, help.worktree); err != nil {
			ws.logger.Error("failed to execute after update", zap.Error(err))
			return err
		}
	}

	data := TemplateData{
		ComposerDiff:           help.table,
		DependencyUpdateReport: help.updateReport,
		UpdateHooks:            updateHooks,
	}

	description, err := ws.GenerateDescription(data, "dependency_update.go.tmpl")
	if err != nil {
		ws.logger.Error("failed to generate description", zap.Error(err))
		return err
	}

	if !ws.config.DryRun {
		if err = ws.PushChanges(help.repository, help.updateBranchName); err != nil {
			return err
		}

		ws.logger.Debug("creating merge request", zap.String("title", "Drupal Maintenance Updates"), zap.String("description", description), zap.String("sourceBranch", help.updateBranchName), zap.String("targetBranch", ws.config.Branch))
		codehostingPlatform := ws.vcsProviderFactory.Create(ws.config.RepositoryURL, ws.config.Token)
		title := fmt.Sprintf("%s: Drupal Maintenance Updates", time.Now().Format("January 2006"))
		mr, err := codehostingPlatform.CreateMergeRequest(title, description, help.updateBranchName, ws.config.Branch)
		if err != nil {
			ws.logger.Error("failed to create merge request", zap.Error(err))
			// remove the branch if the merge request creation failed
			//worktree.
		}
		ws.logger.Info("merge request created", zap.String("url", mr.URL))
	}

	tmpDirName := fmt.Sprintf("/tmp/%x", md5.Sum([]byte(ws.config.RepositoryURL)))
	os.RemoveAll(tmpDirName)

	return nil
}
