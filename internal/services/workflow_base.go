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
	start := time.Now()

	// Bound the whole run so a wedged subprocess or network call can't hang forever.
	if ws.config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, ws.config.Timeout)
		defer cancel()
	}

	username, email := ws.platform.GetUser(ctx)

	// Acquire a single working directory: the existing checkout (default, CI) or a fresh
	// clone (--clone, for local testing). Old and new code live in this one directory
	// sequentially: install the baseline site, then composer update, then run update hooks.
	repository, worktree, path, err := ws.acquireWorkingCopy(ctx, username, email)
	if err != nil {
		return err
	}
	defer func() {
		ws.logger.Info("update run finished", zap.Duration("duration", time.Since(start)))
		ws.cleanup(path)
	}()

	// Fail fast if the runtime PHP/extensions don't satisfy the project's platform
	// requirements; composer update would otherwise fail mid-run with confusing output.
	if out, err := ws.composer.CheckPlatformReqs(ctx, path); err != nil {
		return fmt.Errorf("PHP platform requirements not satisfied:\n%s", out)
	}

	ws.logger.Info("running composer install")
	if err := ws.composer.Install(ctx, path); err != nil {
		return fmt.Errorf("failed to run composer install: %w", err)
	}

	// Install each site at the current (old) code to create the baseline database.
	if err := ws.forEachSite(ctx, func(ctx context.Context, site string) error {
		if err := ws.installer.Install(ctx, path, site); err != nil {
			return fmt.Errorf("site %s installation failed: %w", site, err)
		}
		return nil
	}); err != nil {
		return err
	}

	// Update the shared code: composer update, commit, and create the update branch.
	updateBranchName, err := ws.updateSharedCode(ctx, repository, worktree, path)
	if err != nil {
		return err
	}

	// Run the update hooks and export config per site against the now-updated code.
	if err := ws.forEachSite(ctx, func(ctx context.Context, site string) error {
		return ws.updateSite(ctx, path, worktree, site)
	}); err != nil {
		return err
	}

	if !ws.config.DryRun {
		return ws.publishWork(ctx, repository, updateBranchName, addons)
	}
	return nil
}

// acquireWorkingCopy returns the single working directory the run operates on. By default it
// opens the existing checkout in place; with --clone it clones the repository to a temp dir.
func (ws *WorkflowBaseService) acquireWorkingCopy(ctx context.Context, username, email string) (GitRepository, Worktree, string, error) {
	if ws.config.Clone {
		ws.logger.Info("cloning repository", zap.String("url", ws.config.RepositoryURL), zap.String("branch", ws.config.Branch))
		repository, worktree, path, err := ws.repository.CloneRepository(ws.config.RepositoryURL, ws.config.Branch, ws.config.Token, username, email)
		if err != nil {
			return nil, nil, "", fmt.Errorf("failed to clone repository: %w", err)
		}
		return repository, worktree, path, nil
	}

	repository, worktree, path, err := ws.repository.OpenRepository(ws.config.WorkingDir, username, email)
	if err != nil {
		return nil, nil, "", fmt.Errorf("failed to open checkout: %w", err)
	}
	return repository, worktree, path, nil
}

// forEachSite runs fn for every configured site concurrently, bounded to the CPU count, and
// cancels the rest on the first error.
func (ws *WorkflowBaseService) forEachSite(ctx context.Context, fn func(context.Context, string) error) error {
	g, groupCtx := errgroup.WithContext(ctx)
	g.SetLimit(runtime.NumCPU())
	for _, site := range ws.config.Sites {
		g.Go(func() error {
			return fn(groupCtx, site)
		})
	}
	return g.Wait()
}

// cleanup removes the artifacts the run created. In clone mode that's the whole temp clone;
// in checkout mode it's the SQLite databases and private files written beside the checkout.
func (ws *WorkflowBaseService) cleanup(path string) {
	if ws.config.Clone {
		tmpDirName := filepath.Join(os.TempDir(), fmt.Sprintf("%x", md5.Sum([]byte(ws.config.RepositoryURL))))
		os.RemoveAll(tmpDirName)
		return
	}
	parent := filepath.Dir(path)
	for _, site := range ws.config.Sites {
		os.Remove(filepath.Join(parent, site+".sqlite"))
	}
	os.RemoveAll(filepath.Join(parent, "private"))
}

func (ws *WorkflowBaseService) updateSharedCode(ctx context.Context, repository GitRepository, worktree Worktree, path string) (string, error) {
	ws.logger.Info("updating dependencies")

	preComposerUpdateEvent := NewPreComposerUpdateEvent(ctx, path, worktree, []string{}, []string{}, false)
	if err := ws.dispatcher.FireEvent(preComposerUpdateEvent); err != nil {
		return "", fmt.Errorf("failed to fire event: %w", err)
	}

	changes, err := ws.composer.Update(ctx, path, preComposerUpdateEvent.PackagesToUpdate, preComposerUpdateEvent.PackagesToKeep, preComposerUpdateEvent.MinimalChanges, false)
	if err != nil {
		return "", fmt.Errorf("failed to update dependencies: %w", err)
	}
	if len(changes) == 0 {
		return "", AbortError{Msg: "no changes detected"}
	}

	// Summarise the dependency changes for the run log.
	byAction := map[string]int{}
	for _, c := range changes {
		byAction[c.Action]++
	}
	ws.logger.Info("dependencies updated",
		zap.Int("total", len(changes)),
		zap.Int("installed", byAction["Install"]),
		zap.Int("upgraded", byAction["Upgrade"]),
		zap.Int("downgraded", byAction["Downgrade"]),
		zap.Int("removed", byAction["Remove"]),
	)

	postComposerUpdateEvent := NewPostComposerUpdateEvent(ctx, path, worktree)
	if err := ws.dispatcher.FireEvent(postComposerUpdateEvent); err != nil {
		return "", fmt.Errorf("failed to fire event: %w", err)
	}

	if err := worktree.AddGlob("composer.*"); err != nil {
		return "", fmt.Errorf("failed to add composer.* files: %w", err)
	}
	if _, err := worktree.Commit("Update composer.json and composer.lock", &git.CommitOptions{}); err != nil {
		return "", fmt.Errorf("failed to commit composer.json and composer.lock: %w", err)
	}

	postCodeUpdateEvent := NewPostCodeUpdateEvent(ctx, path, worktree)
	if err := ws.dispatcher.FireEvent(postCodeUpdateEvent); err != nil {
		return "", fmt.Errorf("failed to fire event: %w", err)
	}

	// Get composer lock hash for branch name
	composerLockHash, err := ws.composer.GetLockHash(path)
	if err != nil {
		return "", err
	}

	updateBranchName := fmt.Sprintf("update-%s", composerLockHash)

	// Check if branch already exists
	exists, err := ws.repository.BranchExists(repository, updateBranchName)
	if err != nil {
		return "", fmt.Errorf("failed to check if branch exists: %w", err)
	}
	if exists {
		return "", AbortError{Msg: fmt.Sprintf("branch %s already exists, skipping", updateBranchName)}
	}

	// Create final branch for changes
	if err := worktree.Checkout(&git.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName(updateBranchName),
		Create: true,
		Force:  false,
		Keep:   true,
	}); err != nil {
		return "", fmt.Errorf("failed to checkout branch: %w", err)
	}

	return updateBranchName, nil
}

func (ws *WorkflowBaseService) updateSite(ctx context.Context, path string, worktree Worktree, site string) error {
	ws.logger.Info("updating site", zap.String("site", site))

	if err := ws.installer.ConfigureDatabase(ctx, path, site); err != nil {
		return fmt.Errorf("failed to configure database: %w", err)
	}

	preSiteUpdateEvent := NewPreSiteUpdateEvent(ctx, path, worktree, site)
	if err := ws.dispatcher.FireEvent(preSiteUpdateEvent); err != nil {
		return fmt.Errorf("failed to fire event: %w", err)
	}

	if err := ws.drush.UpdateSite(ctx, path, site); err != nil {
		return fmt.Errorf("failed to update site: %w", err)

	}

	if err := ws.drush.ConfigResave(ctx, path, site); err != nil {
		return fmt.Errorf("failed to resave config: %w", err)

	}

	postSiteUpdateEvent := NewPostSiteUpdateEvent(ctx, path, worktree, site)
	if err := ws.dispatcher.FireEvent(postSiteUpdateEvent); err != nil {
		return fmt.Errorf("failed to fire event: %w", err)
	}

	ws.logger.Info("exporting configuration", zap.String("site", site))
	if err := ws.drush.ExportConfiguration(ctx, path, site); err != nil {
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
