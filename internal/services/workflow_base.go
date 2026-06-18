package services

import (
	"bytes"
	"context"
	"crypto/md5"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"text/template"
	"time"

	"github.com/drupdater/drupdater/internal"

	git "github.com/go-git/go-git/v5"
	gitConfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

//go:embed templates
var templates embed.FS

type TemplateData struct {
	Addons []internal.Addon
}

type SharedUpdate struct {
	Path             string
	Worktree         Worktree
	Repository       GitRepository
	updateBranchName string
}

type WorkflowBaseService struct {
	logger     *zap.Logger
	config     internal.Config
	drush      Drush
	platform   Platform
	repository Repository
	installer  Installer
	composer   Composer
	dispatcher EventDispatcher
	current    time.Time
}

func NewWorkflowBaseService(
	logger *zap.Logger,
	config internal.Config,
	drush Drush,
	platform Platform,
	repository Repository,
	installer Installer,
	composerService Composer,
	dispatcher EventDispatcher,
) *WorkflowBaseService {
	return &WorkflowBaseService{
		logger:     logger,
		config:     config,
		drush:      drush,
		platform:   platform,
		repository: repository,
		installer:  installer,
		composer:   composerService,
		dispatcher: dispatcher,
		current:    time.Now(),
	}
}

func (ws *WorkflowBaseService) StartUpdate(ctx context.Context, addons []internal.Addon) error {
	defer func() {
		// Clean up the temporary directory
		tmpDirName := filepath.Join(os.TempDir(), fmt.Sprintf("%x", md5.Sum([]byte(ws.config.RepositoryURL))))
		os.RemoveAll(tmpDirName)
	}()

	// Bound the whole run so a wedged subprocess or network call can't hang forever.
	if ws.config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, ws.config.Timeout)
		defer cancel()
	}

	// errgroup cancels ctx on the first error and bounds concurrency to the CPU count.
	// publishWork runs only after Wait() returns nil, so a failed phase can never publish an MR.
	g, groupCtx := errgroup.WithContext(ctx)
	g.SetLimit(runtime.NumCPU())

	// installCode and updateSharedCode are independent; site work waits on these signals.
	// Closing a channel after assigning the result establishes a happens-before edge for the read.
	var installPath string
	installCodeDone := make(chan struct{})
	g.Go(func() error {
		path, err := ws.installCode(groupCtx)
		if err != nil {
			return fmt.Errorf("code installation failed: %w", err)
		}
		installPath = path
		close(installCodeDone)
		return nil
	})

	var sharedUpdate SharedUpdate
	updateDone := make(chan struct{})
	g.Go(func() error {
		update, err := ws.updateSharedCode(groupCtx)
		if err != nil {
			return err
		}
		sharedUpdate = update
		close(updateDone)
		return nil
	})

	// Per site: install (after installCode), then update (after install + shared update).
	for _, site := range ws.config.Sites {
		g.Go(func() error {
			select {
			case <-installCodeDone:
			case <-groupCtx.Done():
				return groupCtx.Err()
			}

			if err := ws.installer.Install(groupCtx, installPath, site); err != nil {
				return fmt.Errorf("site %s installation failed: %w", site, err)
			}

			select {
			case <-updateDone:
			case <-groupCtx.Done():
				return groupCtx.Err()
			}

			return ws.updateSite(groupCtx, sharedUpdate, site)
		})
	}

	if err := g.Wait(); err != nil {
		return err
	}

	if !ws.config.DryRun {
		return ws.publishWork(ctx, sharedUpdate.Repository, sharedUpdate.updateBranchName, addons)
	}
	return nil
}

func (ws *WorkflowBaseService) installCode(ctx context.Context) (string, error) {
	username, email := ws.platform.GetUser(ctx)
	ws.logger.Info("cloning repository for site-install", zap.String("url", ws.config.RepositoryURL), zap.String("branch", ws.config.Branch))
	_, _, path, err := ws.repository.CloneRepository(ws.config.RepositoryURL, ws.config.Branch, ws.config.Token, username, email)
	if err != nil {
		return "", fmt.Errorf("failed to clone repository: %w", err)
	}

	ws.logger.Info("running composer install")
	if err = ws.composer.Install(ctx, path); err != nil {
		return "", fmt.Errorf("failed to run composer install: %w", err)
	}

	return path, nil
}

func (ws *WorkflowBaseService) updateSharedCode(ctx context.Context) (SharedUpdate, error) {
	ws.logger.Info("cloning repository for update", zap.String("url", ws.config.RepositoryURL), zap.String("branch", ws.config.Branch))
	username, email := ws.platform.GetUser(ctx)
	repository, worktree, path, err := ws.repository.CloneRepository(ws.config.RepositoryURL, ws.config.Branch, ws.config.Token, username, email)
	if err != nil {
		return SharedUpdate{}, fmt.Errorf("failed to clone repository: %w", err)
	}

	// Fail fast if the runtime PHP/extensions don't satisfy the project's platform
	// requirements; composer update would otherwise fail mid-run with confusing output.
	if out, err := ws.composer.CheckPlatformReqs(ctx, path); err != nil {
		return SharedUpdate{}, fmt.Errorf("PHP platform requirements not satisfied:\n%s", out)
	}

	ws.logger.Info("updating dependencies")

	preComposerUpdateEvent := NewPreComposerUpdateEvent(ctx, path, worktree, []string{}, []string{}, false)
	err = ws.dispatcher.FireEvent(preComposerUpdateEvent)
	if err != nil {
		return SharedUpdate{}, fmt.Errorf("failed to fire event: %w", err)
	}

	changes, err := ws.composer.Update(ctx, path, preComposerUpdateEvent.PackagesToUpdate, preComposerUpdateEvent.PackagesToKeep, preComposerUpdateEvent.MinimalChanges, false)
	if err != nil {
		return SharedUpdate{}, fmt.Errorf("failed to update dependencies: %w", err)
	}
	if len(changes) == 0 {
		return SharedUpdate{}, AbortError{Msg: "no changes detected"}
	}

	postComposerUpdateEvent := NewPostComposerUpdateEvent(ctx, path, worktree)
	err = ws.dispatcher.FireEvent(postComposerUpdateEvent)
	if err != nil {
		return SharedUpdate{}, fmt.Errorf("failed to fire event: %w", err)
	}

	err = worktree.AddGlob("composer.*")
	if err != nil {
		return SharedUpdate{}, fmt.Errorf("failed to add composer.* files: %w", err)
	}
	if _, err := worktree.Commit("Update composer.json and composer.lock", &git.CommitOptions{}); err != nil {
		return SharedUpdate{}, fmt.Errorf("failed to commit composer.json and composer.lock: %w", err)
	}

	postCodeUpdateEvent := NewPostCodeUpdateEvent(ctx, path, worktree)
	err = ws.dispatcher.FireEvent(postCodeUpdateEvent)
	if err != nil {
		return SharedUpdate{}, fmt.Errorf("failed to fire event: %w", err)
	}

	// Get composer lock hash for branch name
	composerLockHash, err := ws.composer.GetLockHash(path)
	if err != nil {
		return SharedUpdate{}, err
	}

	updateBranchName := fmt.Sprintf("update-%s", composerLockHash)

	// Check if branch already exists
	exists, err := ws.repository.BranchExists(repository, updateBranchName)
	if err != nil {
		return SharedUpdate{}, fmt.Errorf("failed to check if branch exists: %w", err)
	}
	if exists {
		return SharedUpdate{}, AbortError{Msg: fmt.Sprintf("branch %s already exists, skipping", updateBranchName)}
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

	sharedUpdate := SharedUpdate{
		Path:             path,
		Worktree:         worktree,
		Repository:       repository,
		updateBranchName: updateBranchName,
	}
	return sharedUpdate, nil
}

func (ws *WorkflowBaseService) updateSite(ctx context.Context, sharedUpdate SharedUpdate, site string) error {
	ws.logger.Info("updating site", zap.String("site", site))

	if err := ws.installer.ConfigureDatabase(ctx, sharedUpdate.Path, site); err != nil {
		return fmt.Errorf("failed to configure database: %w", err)
	}

	preSiteUpdateEvent := NewPreSiteUpdateEvent(ctx, sharedUpdate.Path, sharedUpdate.Worktree, site)
	if err := ws.dispatcher.FireEvent(preSiteUpdateEvent); err != nil {
		return fmt.Errorf("failed to fire event: %w", err)
	}

	if err := ws.drush.UpdateSite(ctx, sharedUpdate.Path, site); err != nil {
		return fmt.Errorf("failed to update site: %w", err)

	}

	if err := ws.drush.ConfigResave(ctx, sharedUpdate.Path, site); err != nil {
		return fmt.Errorf("failed to resave config: %w", err)

	}

	postSiteUpdateEvent := NewPostSiteUpdateEvent(ctx, sharedUpdate.Path, sharedUpdate.Worktree, site)
	if err := ws.dispatcher.FireEvent(postSiteUpdateEvent); err != nil {
		return fmt.Errorf("failed to fire event: %w", err)
	}

	ws.logger.Info("exporting configuration", zap.String("site", site))
	if err := ws.drush.ExportConfiguration(ctx, sharedUpdate.Path, site); err != nil {
		return fmt.Errorf("failed to export configuration: %w", err)
	}

	return nil
}

func (ws *WorkflowBaseService) publishWork(ctx context.Context, repository GitRepository, updateBranchName string, addons []internal.Addon) error {
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

	title := fmt.Sprintf("%s: Drupal Maintenance Updates", ws.current.Format("January 2006"))

	data := TemplateData{
		Addons: addons,
	}

	// Generate description and create MR
	description, err := ws.GenerateDescription(data, "dependency_update.go.tmpl")
	if err != nil {
		return fmt.Errorf("failed to generate description: %w", err)
	}

	e := NewPreMergeRequestCreateEvent(title)
	err = ws.dispatcher.FireEvent(e)
	if err != nil {
		return fmt.Errorf("failed to fire event: %w", err)
	}

	mr, err := ws.platform.CreateMergeRequest(ctx, e.Title, description, updateBranchName, ws.config.Branch)
	if err != nil {
		return fmt.Errorf("failed to create merge request: %w", err)
	}
	ws.logger.Info("merge request created", zap.String("url", mr.URL))

	return nil
}

func (ws *WorkflowBaseService) GenerateDescription(data any, filename string) (string, error) {
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
