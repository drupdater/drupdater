package services

import (
	"crypto/md5"
	"fmt"
	"os"
	"slices"
	"sync"
	"time"

	"drupdater/internal"
	"drupdater/internal/codehosting"
	"drupdater/internal/utils"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"go.uber.org/zap"
)

type WorkflowSecurityUpdateService struct {
	WorkflowBaseService
	installer          InstallerService
	updater            UpdaterService
	repository         RepositoryService
	vcsProviderFactory codehosting.VcsProviderFactory
	commandExecutor    utils.CommandExecutor
	composerService    ComposerService
}

func newWorkflowSecurityUpdateService(logger *zap.Logger, installer InstallerService, updater UpdaterService, repository RepositoryService, vcsProviderFactory codehosting.VcsProviderFactory, config internal.Config, commandExecutor utils.CommandExecutor, composerService ComposerService) *WorkflowSecurityUpdateService {
	return &WorkflowSecurityUpdateService{
		WorkflowBaseService: WorkflowBaseService{
			logger: logger,
			config: config,
		},
		installer:          installer,
		updater:            updater,
		repository:         repository,
		vcsProviderFactory: vcsProviderFactory,
		commandExecutor:    commandExecutor,
		composerService:    composerService,
	}
}

func (ws *WorkflowSecurityUpdateService) StartUpdate() error {
	ws.logger.Info("starting security update workflow")

	ws.logger.Info("cloning repository for update", zap.String("repositoryURL", ws.config.RepositoryURL), zap.String("branch", ws.config.Branch))
	repository, worktree, path, err := ws.repository.CloneRepository(ws.config.RepositoryURL, ws.config.Branch, ws.config.Token)
	if err != nil {
		return err
	}

	beforeUpdateAudit, err := ws.composerService.RunComposerAudit(path)
	if err != nil {
		return err
	}

	if len(beforeUpdateAudit.Advisories) == 0 {
		ws.logger.Info("no security advisories found, skipping security update")
		return nil
	}
	ws.logger.Info("found security advisories", zap.Int("numAdvisories", len(beforeUpdateAudit.Advisories)))
	ws.logger.Info("advisories", zap.Any("advisories", beforeUpdateAudit.Advisories))

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

	packagesToUpdate := make([]string, 0)
	for _, advisory := range beforeUpdateAudit.Advisories {
		if slices.Contains(packagesToUpdate, advisory.PackageName) {
			continue
		}
		packagesToUpdate = append(packagesToUpdate, advisory.PackageName)
	}

	beforeUpdateCommit, _ := ws.repository.GetHeadCommit(repository)
	if slices.Contains(packagesToUpdate, "drupal/core") {
		packagesToUpdate = append(packagesToUpdate, "drupal/core-recommended")
		packagesToUpdate = append(packagesToUpdate, "drupal/core-composer-scaffold")
	}
	ws.logger.Info("updating dependencies", zap.Strings("packages", packagesToUpdate))
	updateReport, err := ws.updater.UpdateDependencies(path, packagesToUpdate, worktree, true)
	if err != nil {
		return err
	}

	table, err := ws.commandExecutor.GenerateDiffTable(path, beforeUpdateCommit.Hash.String(), true)
	if err != nil {
		return err
	}

	if table == "" {
		ws.logger.Info("no packages were updated, skipping security update")
		return nil
	}

	composerLockHash, err := ws.composerService.GetComposerLockHash(path)
	if err != nil {
		return err
	}
	updateBranchName := fmt.Sprintf("security-update-%s", composerLockHash)

	if exists, _ := ws.repository.BranchExists(repository, updateBranchName); exists {
		ws.logger.Info("branch already exists", zap.String("branch", updateBranchName))
		return nil
	}

	if err := worktree.Checkout(&git.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName(updateBranchName),
		Create: true,
		Force:  false,
		Keep:   true,
	}); err != nil {
		ws.logger.Error("failed to checkout branch", zap.Error(err))
		return err
	}

	afterUpdateAudit, err := ws.composerService.RunComposerAudit(path)
	if err != nil {
		return err
	}

	wg.Wait()
	if len(errChannel) > 0 {
		return <-errChannel
	}

	updateHooks, err := ws.updater.UpdateDrupal(path, worktree, ws.config.Sites)
	if err != nil {
		return err
	}

	data := TemplateData{
		ComposerDiff:           table,
		DependencyUpdateReport: updateReport,
		SecurityReport: SecurityReport{
			FixedAdvisories:       ws.GetFixedAdvisories(beforeUpdateAudit.Advisories, afterUpdateAudit.Advisories),
			AfterUpdateAdvisories: afterUpdateAudit.Advisories,
			NumUnresolvedIssues:   len(afterUpdateAudit.Advisories),
		},
		UpdateHooks: updateHooks,
	}

	description, err := ws.GenerateDescription(data, "security_update.go.tmpl")
	if err != nil {
		ws.logger.Error("failed to generate description", zap.Error(err))
	}

	if !ws.config.DryRun {
		if err = ws.PushChanges(repository, updateBranchName); err != nil {
			return err
		}

		codehostingPlatform := ws.vcsProviderFactory.Create(ws.config.RepositoryURL, ws.config.Token)
		title := fmt.Sprintf("%s: Drupal Security Updates", time.Now().Format("2006-01-02"))
		mr, err := codehostingPlatform.CreateMergeRequest(title, description, updateBranchName, ws.config.Branch)
		if err != nil {
			ws.logger.Error("failed to create merge request", zap.Error(err))
		}
		ws.logger.Info("merge request created", zap.String("url", mr.URL))
	}

	tmpDirName := fmt.Sprintf("/tmp/%x", md5.Sum([]byte(ws.config.RepositoryURL)))
	os.RemoveAll(tmpDirName)

	return nil
}

func (ws *WorkflowSecurityUpdateService) GetFixedAdvisories(before []Advisory, after []Advisory) []Advisory {
	// Get advisories from before that are not present in after
	var fixed = make([]Advisory, 0)
	for _, beforeAdvisory := range before {
		found := false
		for _, afterAdvisory := range after {
			if beforeAdvisory.CVE == afterAdvisory.CVE {
				found = true
				break
			}
		}
		if !found {
			fixed = append(fixed, beforeAdvisory)
		}
	}
	return fixed

}
