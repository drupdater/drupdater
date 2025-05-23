package services

import (
	"bytes"
	"context"
	"crypto/md5"
	"embed"
	"fmt"
	"os"
	"runtime"
	"sync"
	"text/template"
	"time"

	"github.com/drupdater/drupdater/internal"
	"github.com/gookit/event"

	git "github.com/go-git/go-git/v5"
	gitConfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"go.uber.org/zap"
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
) *WorkflowBaseService {
	return &WorkflowBaseService{
		logger:     logger,
		config:     config,
		drush:      drush,
		platform:   platform,
		repository: repository,
		installer:  installer,
		composer:   composerService,
		current:    time.Now(),
	}
}

func (ws *WorkflowBaseService) StartUpdate(ctx context.Context, addons []internal.Addon) error {
	ctx, cancel := context.WithCancel(context.Background())

	defer func() {
		// Clean up the temporary directory
		tmpDirName := fmt.Sprintf("/tmp/%x", md5.Sum([]byte(ws.config.RepositoryURL)))
		os.RemoveAll(tmpDirName)

		cancel()
	}()

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

	// Limit concurrency to number of CPU cores
	cpuLimit := runtime.NumCPU()
	sem := make(chan struct{}, cpuLimit)

	// 1. Run installCode()
	wg.Add(1)
	go func() {
		sem <- struct{}{}
		defer func() {
			<-sem
			wg.Done()
		}()

		path, err := ws.installCode(ctx)
		if err != nil {
			errCh <- fmt.Errorf("code installation failed: %w", err)
			cancel()
			return
		}

		// Send path to all waiting goroutines
		installCodeDone <- path
	}()

	// 2. Run installSite(site) after installCode
	for _, site := range ws.config.Sites {
		wg.Add(1)
		go func(site string) {
			sem <- struct{}{}
			defer func() {
				<-sem
				wg.Done()
			}()

			select {
			case installPath := <-installCodeDone:
				// Put the path back for other goroutines
				installCodeDone <- installPath

				if err := ws.installer.Install(ctx, installPath, site); err != nil {
					errCh <- fmt.Errorf("site %s installation failed: %w", site, err)
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
		sem <- struct{}{}
		defer func() {
			<-sem
			wg.Done()
		}()

		var err error
		update, err := ws.updateSharedCode(ctx)
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
			sem <- struct{}{}
			defer func() {
				<-sem
				wg.Done()
			}()

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

			err := ws.updateSite(ctx, sharedUpdate, site)
			if err != nil {
				errCh <- err
				cancel()
				return
			}

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
			return ws.publishWork(sharedUpdate.Repository, sharedUpdate.updateBranchName, addons)
		}
		return nil
	case err := <-errCh:
		return err
	}
}

func (ws *WorkflowBaseService) installCode(ctx context.Context) (string, error) {
	username, email := ws.platform.GetUser()
	ws.logger.Info("cloning repository for site-install", zap.String("repositoryURL", ws.config.RepositoryURL), zap.String("branch", ws.config.Branch))
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
	ws.logger.Info("cloning repository for update", zap.String("repositoryURL", ws.config.RepositoryURL), zap.String("branch", ws.config.Branch))
	username, email := ws.platform.GetUser()
	repository, worktree, path, err := ws.repository.CloneRepository(ws.config.RepositoryURL, ws.config.Branch, ws.config.Token, username, email)
	if err != nil {
		return SharedUpdate{}, fmt.Errorf("failed to clone repository: %w", err)
	}

	ws.logger.Info("updating dependencies")

	preComposerUpdateEvent := NewPreComposerUpdateEvent(ctx, path, worktree, []string{}, []string{}, false)
	err = event.FireEvent(preComposerUpdateEvent)
	if err != nil {
		return SharedUpdate{}, fmt.Errorf("failed to fire event: %w", err)
	}

	changes, err := ws.composer.Update(ctx, path, preComposerUpdateEvent.PackagesToUpdate, preComposerUpdateEvent.PackagesToKeep, preComposerUpdateEvent.MinimalChanges, false)
	if err != nil {
		return SharedUpdate{}, fmt.Errorf("failed to update dependencies: %w", err)
	}
	if len(changes) == 0 {
		ws.logger.Warn("no changes detected")
		return SharedUpdate{}, nil
	}

	postComposerUpdateEvent := NewPostComposerUpdateEvent(ctx, path, worktree)
	err = event.FireEvent(postComposerUpdateEvent)
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
	err = event.FireEvent(postCodeUpdateEvent)
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
	if err := event.FireEvent(preSiteUpdateEvent); err != nil {
		return fmt.Errorf("failed to fire event: %w", err)
	}

	if err := ws.drush.UpdateSite(ctx, sharedUpdate.Path, site); err != nil {
		return fmt.Errorf("failed to update site: %w", err)

	}

	if err := ws.drush.ConfigResave(ctx, sharedUpdate.Path, site); err != nil {
		return fmt.Errorf("failed to resave config: %w", err)

	}

	postSiteUpdateEvent := NewPostSiteUpdateEvent(ctx, sharedUpdate.Path, sharedUpdate.Worktree, site)
	if err := event.FireEvent(postSiteUpdateEvent); err != nil {
		return fmt.Errorf("failed to fire event: %w", err)
	}

	ws.logger.Info("export configuration", zap.String("site", site))
	if err := ws.drush.ExportConfiguration(ctx, sharedUpdate.Path, site); err != nil {
		return fmt.Errorf("failed to export configuration: %w", err)
	}

	return nil
}

func (ws *WorkflowBaseService) publishWork(repository GitRepository, updateBranchName string, addons []internal.Addon) error {
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
	err = event.FireEvent(e)
	if err != nil {
		return fmt.Errorf("failed to fire event: %w", err)
	}

	mr, err := ws.platform.CreateMergeRequest(e.Title, description, updateBranchName, ws.config.Branch)
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
