package services

import (
	"crypto/md5"
	"fmt"
	"os"
	"sync"
	"time"
	"unicode/utf8"

	"ebersolve.com/updater/internal"
	"ebersolve.com/updater/internal/codehosting"
	"ebersolve.com/updater/internal/utils"
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

func (ws *WorkflowDependencyUpdateService) StartUpdate() error {
	var wg sync.WaitGroup
	wg.Add(1)
	errChannel := make(chan error, 1)

	go func() {
		defer wg.Done()
		err := ws.installer.InstallDrupal(ws.config.RepositoryURL, ws.config.Branch, ws.config.Token, ws.config.Sites)
		if err != nil {
			errChannel <- err
			ws.logger.Error("failed to install Drupal", zap.Error(err))
		}
	}()

	updateBranchName := fmt.Sprintf("update-%s", time.Now().Format("20060102150405"))
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

	beforeUpdateCommit, _ := ws.repository.GetHeadCommit(repository)
	updateReport, err := ws.updater.UpdateDependencies(path, []string{}, worktree, false)
	if err != nil {
		return err
	}

	table, err := ws.commandExecutor.GenerateDiffTable(path, beforeUpdateCommit.Hash.String(), true)
	if err != nil {
		return err
	}

	if table == "" {
		ws.logger.Info("no packages were updated, skipping update")
		return nil
	}

	tableCharCount := utf8.RuneCountInString(table)
	if tableCharCount > 63000 {
		table, err = ws.commandExecutor.GenerateDiffTable(path, beforeUpdateCommit.Hash.String(), false)
		if err != nil {
			return err
		}
	}

	wg.Wait()
	if len(errChannel) > 0 {
		return <-errChannel
	}

	updateHooks, err := ws.updater.UpdateDrupal(path, worktree, ws.config.Sites)
	if err != nil {
		return err
	}

	for _, au := range ws.afterUpdate {
		if err := au.Execute(path, worktree); err != nil {
			ws.logger.Error("failed to execute after update", zap.Error(err))
			return err
		}
	}

	data := TemplateData{
		ComposerDiff:           table,
		DependencyUpdateReport: updateReport,
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

		gitlab := ws.vcsProviderFactory.Create(ws.config.RepositoryURL, ws.config.Token)
		title := fmt.Sprintf("%s: Drupal Maintenance Updates", time.Now().Format("January 2006"))
		if err = gitlab.CreateMergeRequest(title, description, updateBranchName, ws.config.Branch); err != nil {
			ws.logger.Error("failed to create merge request", zap.Error(err))
			// remove the branch if the merge request creation failed
			//worktree.
		}
	}

	tmpDirName := fmt.Sprintf("/tmp/%x", md5.Sum([]byte(ws.config.RepositoryURL)))
	os.RemoveAll(tmpDirName)

	return nil
}
