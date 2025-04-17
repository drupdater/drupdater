package services

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/drupdater/drupdater/internal"
	"github.com/drupdater/drupdater/internal/codehosting"
	"github.com/drupdater/drupdater/internal/utils"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"go.uber.org/zap"
)

type WorkflowSecurityUpdateService struct {
	WorkflowBaseService
	installer       InstallerService
	repository      RepositoryService
	composerService ComposerService
}

func newWorkflowSecurityUpdateService(logger *zap.Logger, installer InstallerService, updater UpdaterService, repository RepositoryService, vcsProviderFactory codehosting.VcsProviderFactory, config internal.Config, commandExecutor utils.CommandExecutor, composerService ComposerService) *WorkflowSecurityUpdateService {
	return &WorkflowSecurityUpdateService{
		WorkflowBaseService: WorkflowBaseService{
			logger:             logger,
			config:             config,
			updater:            updater,
			commandExecutor:    commandExecutor,
			vcsProviderFactory: vcsProviderFactory,
		},
		installer:       installer,
		repository:      repository,
		composerService: composerService,
	}
}

func (ws *WorkflowSecurityUpdateService) StartUpdate(ctx context.Context) error {
	ws.logger.Info("starting security update workflow")

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

	ws.logger.Info("cloning repository for update", zap.String("repositoryURL", ws.config.RepositoryURL), zap.String("branch", ws.config.Branch))
	repository, worktree, path, err := ws.repository.CloneRepository(ws.config.RepositoryURL, ws.config.Branch, ws.config.Token)
	if err != nil {
		return err
	}

	beforeUpdateAudit, err := ws.composerService.RunComposerAudit(ctx, path)
	if err != nil {
		return err
	}

	packagesToUpdate, err := ws.getPackagesToUpdate(ctx, path, beforeUpdateAudit.Advisories)
	if err != nil {
		return err
	}

	if len(packagesToUpdate) == 0 {
		ws.logger.Info("no security advisories found, skipping security update")
		return nil
	}

	go ws.Update(errCh, resultCh, ctx, packagesToUpdate, true, path, worktree, &wg)

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

	afterUpdateAudit, err := ws.composerService.RunComposerAudit(ctx, path)
	if err != nil {
		return err
	}

	updateHooks, err := ws.updater.UpdateDrupal(ctx, path, worktree, ws.config.Sites)
	if err != nil {
		return err
	}

	if !ws.config.DryRun {
		if err = ws.PushChanges(repository, updateBranchName); err != nil {
			return err
		}

		data := TemplateData{
			ComposerDiff:           result.table,
			DependencyUpdateReport: result.updateReport,
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

		title := fmt.Sprintf("%s: Drupal Security Updates", time.Now().Format("2006-01-02"))
		ws.CreateMergeRequest(title, description, updateBranchName, ws.config.Branch)
	}

	// Clean up the temporary directory
	defer ws.Cleanup()

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

func (ws *WorkflowSecurityUpdateService) getPackagesToUpdate(ctx context.Context, path string, advisories []Advisory) ([]string, error) {

	ws.logger.Info("found security advisories", zap.Int("numAdvisories", len(advisories)))
	ws.logger.Info("advisories", zap.Any("advisories", advisories))

	packagesToUpdate := make([]string, 0)
	for _, advisory := range advisories {
		if slices.Contains(packagesToUpdate, advisory.PackageName) {
			continue
		}
		packagesToUpdate = append(packagesToUpdate, advisory.PackageName)
	}

	if slices.Contains(packagesToUpdate, "drupal/core") {
		packagesToUpdate = append(packagesToUpdate, "drupal/core-recommended")
		packagesToUpdate = append(packagesToUpdate, "drupal/core-composer-scaffold")
	}

	return packagesToUpdate, nil
}
