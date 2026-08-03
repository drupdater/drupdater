package services

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/drupdater/drupdater/internal"
	"github.com/drupdater/drupdater/internal/report"
	"github.com/drupdater/drupdater/pkg/composer"
	"github.com/drupdater/drupdater/pkg/repo"

	git "github.com/go-git/go-git/v5"
	gitConfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
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

	// siteCommitMu serializes the git-committing tail of updateSite: sites update concurrently
	// but share one working tree and index.
	siteCommitMu sync.Mutex

	// reportSink receives the run report on every exit path. nil when --report was not given.
	reportSink func(report.Report)
}

// Option configures a WorkflowBaseService. Variadic so adding one does not disturb call sites.
type Option func(*WorkflowBaseService)

// WithReportSink hands the finished report to sink once per run, after every other cleanup.
func WithReportSink(sink func(report.Report)) Option {
	return func(ws *WorkflowBaseService) {
		ws.reportSink = sink
	}
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
	opts ...Option,
) *WorkflowBaseService {
	ws := &WorkflowBaseService{
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
	for _, opt := range opts {
		opt(ws)
	}

	return ws
}

func (ws *WorkflowBaseService) StartUpdate(ctx context.Context, addons []internal.Addon) (err error) {
	start := time.Now()

	mode := report.ModeNormal
	if ws.config.Security {
		mode = report.ModeSecurity
	}
	rec := report.NewRecorder(internal.Version, mode, ws.config.DryRun, ws.config.RepositoryURL, ws.config.Branch, ws.config.Sites)

	// Registered first, so it runs last and covers every exit path. An AbortError means there
	// was nothing to do, not that the run failed.
	defer func() {
		if ws.reportSink == nil {
			return
		}
		var abort AbortError
		if errors.As(err, &abort) {
			rec.SetNoChanges()
		}
		rec.AddAddons(addons)
		ws.reportSink(rec.Finish())
	}()

	// Bound the whole run so a wedged subprocess or network call can't hang forever.
	if ws.config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, ws.config.Timeout)
		defer cancel()
	}

	// After the timeout, so the lookup is bounded too. Not a phase: it cannot fail the run.
	rec.SetToolVersions(LookupToolVersions(ctx, ws.logger, ws.composer))

	// platform is nil for a token-free checkout-mode dry run (see tokenRequired), which keeps
	// the checkout's own git identity.
	var username, email string
	if ws.platform != nil {
		username, email = ws.platform.GetUser(ctx)
	}

	// One working directory for the whole run. A phase because a bad token or unreachable host
	// fails here, one of the most common ways a run fails.
	var (
		repository GitRepository
		worktree   Worktree
		path       string
	)
	if err = rec.Run("acquire working copy", func() error {
		var acquireErr error
		repository, worktree, path, acquireErr = ws.acquireWorkingCopy(username, email)
		return acquireErr
	}); err != nil {
		return err
	}

	// Before any branch is created, so a failed run can be put back — see captureOriginalHead.
	originalRef := ws.captureOriginalHead(repository)

	defer func() {
		ws.logger.Info("update run finished", zap.Duration("duration", time.Since(start)))
		if err != nil && originalRef != nil {
			ws.restoreOriginalCheckout(worktree, originalRef)
		}
		ws.cleanup(path)
	}()

	return ws.runPhases(ctx, rec, repository, worktree, path, addons)
}

// runPhases executes the update, one recorded phase at a time. Separate from StartUpdate so the
// run's setup and teardown stay readable next to each other.
func (ws *WorkflowBaseService) runPhases(
	ctx context.Context,
	rec *report.Recorder,
	repository GitRepository,
	worktree Worktree,
	path string,
	addons []internal.Addon,
) error {
	// Fail fast on the prerequisites "drupdater check" shares. Extension requirements are
	// deliberately not among them — see CheckPlatformReqs.
	if err := rec.Run("preflight", func() error {
		if result := CheckGitHistoryComplete(ws.repository, path); !result.OK {
			return fmt.Errorf("%s: %s", result.Name, result.Detail)
		}
		if result := CheckPlatformRequirements(ctx, ws.composer, path); !result.OK {
			return fmt.Errorf("PHP platform requirements not satisfied:\n%s", result.Detail)
		}
		return nil
	}); err != nil {
		return err
	}

	if err := rec.Run("composer install", func() error {
		ws.logger.Info("running composer install")
		if err := ws.composer.Install(ctx, path); err != nil {
			return fmt.Errorf("failed to run composer install: %w", err)
		}
		return nil
	}); err != nil {
		return err
	}

	// Install each site at the current (old) code to create the baseline database.
	if err := rec.Run("baseline site install", func() error {
		return ws.forEachSite(ctx, func(ctx context.Context, site string) error {
			if err := ws.installer.Install(ctx, path, site); err != nil {
				return fmt.Errorf("site %s installation failed: %w", site, err)
			}
			return nil
		})
	}); err != nil {
		return err
	}

	// Update the shared code: composer update, commit, and create the update branch.
	var updateBranchName string
	if err := rec.Run("update shared code", func() error {
		var err error
		updateBranchName, err = ws.updateSharedCode(ctx, repository, worktree, path, rec)
		return err
	}); err != nil {
		return err
	}
	rec.SetUpdateBranch(updateBranchName)

	// Run the update hooks and export config per site against the now-updated code.
	if err := rec.Run("site update", func() error {
		return ws.forEachSite(ctx, func(ctx context.Context, site string) error {
			return ws.updateSite(ctx, path, worktree, site)
		})
	}); err != nil {
		return err
	}

	// Ahead of publish so it runs under --dry-run too: rendered later, a broken template would
	// only surface after the branch had been pushed.
	var mrTitle, mrDescription string
	if err := rec.Run("render merge request", func() error {
		var renderErr error
		mrTitle, mrDescription, renderErr = ws.renderMergeRequest(addons)
		return renderErr
	}); err != nil {
		return err
	}
	rec.SetMergeRequestContent(mrTitle, mrDescription)

	if !ws.config.DryRun {
		return rec.Run("publish", func() error {
			return ws.publishWork(ctx, repository, updateBranchName, mrTitle, mrDescription, rec)
		})
	}
	return nil
}

// renderMergeRequest produces the title and description. The title starts as the maintenance
// default and is offered to the addons — how composer_audit re-labels a security run.
func (ws *WorkflowBaseService) renderMergeRequest(addons []internal.Addon) (string, string, error) {
	e := NewPreMergeRequestCreateEvent(fmt.Sprintf("%s: Drupal Maintenance Updates", ws.current.Format("January 2006")))
	if err := ws.dispatcher.FireEvent(e); err != nil {
		return "", "", fmt.Errorf("failed to fire event: %w", err)
	}

	description, err := ws.GenerateDescription(TemplateData{Addons: addons}, "dependency_update.go.tmpl")
	if err != nil {
		return "", "", fmt.Errorf("failed to generate description: %w", err)
	}

	return e.Title, description, nil
}

// captureOriginalHead returns the checkout's HEAD so a failed run can be put back rather than
// left on drupdater's throwaway work branch. A safety net: nil in clone mode or on failure.
func (ws *WorkflowBaseService) captureOriginalHead(repository GitRepository) *plumbing.Reference {
	if ws.config.Clone {
		return nil
	}
	ref, err := repository.Head()
	if err != nil {
		ws.logger.Warn("failed to determine current HEAD, checkout will not be restored if the run fails", zap.Error(err))
		return nil
	}
	return ref
}

// restoreOriginalCheckout switches worktree back to originalRef, discarding whatever the work
// branch accumulated. Errors are logged, never returned: this must not mask the real failure.
func (ws *WorkflowBaseService) restoreOriginalCheckout(worktree Worktree, originalRef *plumbing.Reference) {
	opts := &git.CheckoutOptions{Force: true}
	if originalRef.Name().IsBranch() {
		opts.Branch = originalRef.Name()
	} else {
		opts.Hash = originalRef.Hash()
	}
	if err := worktree.Checkout(opts); err != nil {
		ws.logger.Warn("failed to restore checkout to its original state", zap.Error(err))
	}
}

// acquireWorkingCopy returns the single working directory the run operates on. By default it
// opens the existing checkout in place; with --clone it clones the repository to a temp dir.
func (ws *WorkflowBaseService) acquireWorkingCopy(username, email string) (GitRepository, Worktree, string, error) {
	if ws.config.Clone {
		ws.logger.Info("cloning repository", zap.String("url", ws.config.RepositoryURL), zap.String("branch", ws.config.Branch))
		return ws.repository.CloneRepository(ws.config.RepositoryURL, ws.config.Branch, ws.config.Token, username, email)
	}
	return ws.repository.OpenRepository(ws.config.WorkingDir, username, email)
}

// stageScaffoldChanges stages the web-root files drupal-scaffold rewrites on a core update --
// .htaccess, robots.txt, index.php -- which the composer.* glob does not cover.
//
// Tracked paths only: vendor/ and web/core legitimately sit untracked in the checkout. Each
// site's settings.php is excluded too, because the installer appends throwaway SQLite
// credentials to it (pkg/drupal/installer.go).
func (ws *WorkflowBaseService) stageScaffoldChanges(ctx context.Context, path string, worktree Worktree) error {
	status, err := worktree.Status()
	if err != nil {
		return fmt.Errorf("failed to read worktree status: %w", err)
	}

	// Sorted so the staging order does not depend on map iteration.
	candidates := make([]string, 0, len(status))
	for file, fileStatus := range status {
		if fileStatus.Worktree == git.Untracked || fileStatus.Worktree == git.Unmodified {
			continue
		}
		candidates = append(candidates, file)
	}
	if len(candidates) == 0 {
		return nil
	}
	slices.Sort(candidates)

	// Looked up only when there is something to stage, to save a composer subprocess.
	webroot, err := composer.WebRoot(ctx, ws.composer, path)
	if err != nil {
		return fmt.Errorf("failed to determine web root: %w", err)
	}
	excluded := make(map[string]struct{}, len(ws.config.Sites))
	for _, site := range ws.config.Sites {
		excluded[filepath.Join(webroot, "sites", site, "settings.php")] = struct{}{}
	}

	staged := make([]string, 0, len(candidates))
	for _, file := range candidates {
		// Web root only: everything outside it belongs to a later phase, and sweeping it in
		// here would commit it mid-update, still carrying the baseline install's state.
		if !strings.HasPrefix(file, webroot+"/") {
			continue
		}
		if _, skip := excluded[file]; skip {
			continue
		}
		if _, err := worktree.Add(file); err != nil {
			return fmt.Errorf("failed to stage %s: %w", file, err)
		}
		staged = append(staged, file)
	}
	if len(staged) > 0 {
		ws.logger.Info("staged scaffold changes", zap.Strings("files", staged))
	}
	return nil
}

// forEachSite runs fn per site concurrently, bounded by config.Concurrency, cancelling the rest
// on the first error.
func (ws *WorkflowBaseService) forEachSite(ctx context.Context, fn func(context.Context, string) error) error {
	g, groupCtx := errgroup.WithContext(ctx)
	limit := ws.config.Concurrency
	if limit <= 0 {
		limit = runtime.GOMAXPROCS(0)
	}
	g.SetLimit(limit)
	for _, site := range ws.config.Sites {
		g.Go(func() error {
			return fn(groupCtx, site)
		})
	}
	return g.Wait()
}

// cleanup removes the run's artifacts: its temp clone, or the files a site install wrote beside
// the checkout.
func (ws *WorkflowBaseService) cleanup(path string) {
	if ws.config.Clone {
		// This run's own directory, not the shared per-URL parent, so a concurrent run of the
		// same repository isn't wiped out. The Rel guard keeps the temp dir itself safe.
		if rel, err := filepath.Rel(os.TempDir(), path); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
			os.RemoveAll(path)
		}
		return
	}
	CleanupSiteArtifacts(filepath.Dir(path), ws.config.Sites)
}

// CleanupSiteArtifacts removes the SQLite databases and private files a site install writes
// beside the checkout. Shared with "drupdater check --full", which owes the same cleanup.
func CleanupSiteArtifacts(parent string, sites []string) {
	for _, site := range sites {
		os.Remove(filepath.Join(parent, site+".sqlite"))
		// Per-site only: "private" is a standard Drupal tree and can hold real project data.
		os.RemoveAll(filepath.Join(parent, "private", site))
	}
	// Drops the parent only if it is now empty — os.Remove refuses a non-empty directory.
	os.Remove(filepath.Join(parent, "private"))
}

func (ws *WorkflowBaseService) updateSharedCode(ctx context.Context, repository GitRepository, worktree Worktree, path string, rec *report.Recorder) (string, error) {
	ws.logger.Info("updating dependencies")

	// A dedicated branch: the addons commit as they go, and a mid-run failure would otherwise
	// strand those commits on the user's own branch. Flat name, not "drupdater/work-<ts>":
	// a branch named "drupdater" makes refs/heads/drupdater a file, blocking nested refs.
	workBranch := fmt.Sprintf("drupdater-work-%d", ws.current.UnixNano())
	if err := worktree.Checkout(&git.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName(workBranch),
		Create: true,
		Keep:   true,
	}); err != nil {
		return "", fmt.Errorf("failed to create work branch: %w", err)
	}

	preComposerUpdateEvent := NewPreComposerUpdateEvent(ctx, path, worktree, []string{}, []string{}, false)
	if err := ws.dispatcher.FireEvent(preComposerUpdateEvent); err != nil {
		return "", fmt.Errorf("failed to fire event: %w", err)
	}

	changes, err := ws.composer.Update(ctx, path, preComposerUpdateEvent.PackagesToUpdate, preComposerUpdateEvent.PackagesToKeep, preComposerUpdateEvent.MinimalChanges, false)
	if err != nil {
		return "", fmt.Errorf("failed to update dependencies: %w", err)
	}
	rec.SetPackages(toReportPackages(changes))
	if len(changes) == 0 {
		return "", AbortError{Msg: "no changes detected"}
	}

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
	if err := ws.stageScaffoldChanges(ctx, path, worktree); err != nil {
		return "", err
	}
	if _, err := worktree.Commit("Update composer.json and composer.lock", &git.CommitOptions{}); err != nil {
		return "", fmt.Errorf("failed to commit composer.json and composer.lock: %w", err)
	}

	postCodeUpdateEvent := NewPostCodeUpdateEvent(ctx, path, worktree)
	if err := ws.dispatcher.FireEvent(postCodeUpdateEvent); err != nil {
		return "", fmt.Errorf("failed to fire event: %w", err)
	}

	composerLockHash, err := ws.composer.GetLockHash(path)
	if err != nil {
		return "", err
	}

	updateBranchName := fmt.Sprintf("update-%s", composerLockHash)

	if err := ws.ensureUpdateBranchAvailable(repository, updateBranchName); err != nil {
		return "", err
	}

	// Create the final branch from the work branch's tip (carrying the accumulated commits).
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

// ensureUpdateBranchAvailable returns an AbortError if updateBranchName is already taken, locally
// or on the remote, and a plain error if either check itself fails.
// The local check runs first: a prior failed run leaves its branch behind, and without it the
// checkout below fails on go-git's raw message instead of a clean AbortError.
func (ws *WorkflowBaseService) ensureUpdateBranchAvailable(repository GitRepository, updateBranchName string) error {
	if _, err := repository.Reference(plumbing.NewBranchReferenceName(updateBranchName), false); err == nil {
		return AbortError{Msg: fmt.Sprintf("branch %s already exists locally, skipping", updateBranchName)}
	} else if !errors.Is(err, plumbing.ErrReferenceNotFound) {
		return fmt.Errorf("failed to check for a local %s branch: %w", updateBranchName, err)
	}

	// Skipped for a dry run: go-git sends an empty password rather than no credential, which
	// hosts reject even for a public repository — breaking the token-free case.
	if ws.config.DryRun {
		return nil
	}

	exists, err := ws.repository.BranchExists(repository, updateBranchName, ws.config.Token)
	if err != nil {
		return fmt.Errorf("failed to check if branch exists: %w", err)
	}
	if exists {
		return AbortError{Msg: fmt.Sprintf("branch %s already exists, skipping", updateBranchName)}
	}
	return nil
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

	// The remaining steps commit into the shared working tree, so one site at a time.
	return ws.commitSiteChanges(ctx, path, worktree, site)
}

// commitSiteChanges holds a lock so concurrently-updated sites don't race on the shared index.
func (ws *WorkflowBaseService) commitSiteChanges(ctx context.Context, path string, worktree Worktree, site string) error {
	ws.siteCommitMu.Lock()
	defer ws.siteCommitMu.Unlock()

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

// toReportPackages converts to the report's own schema type — see report.PackageChange.
func toReportPackages(changes []composer.PackageChange) []report.PackageChange {
	if len(changes) == 0 {
		return nil
	}
	out := make([]report.PackageChange, 0, len(changes))
	for _, c := range changes {
		out = append(out, report.PackageChange{
			Action:  c.Action,
			Package: c.Package,
			From:    c.From,
			To:      c.To,
		})
	}

	return out
}

func (ws *WorkflowBaseService) publishWork(ctx context.Context, repository GitRepository, updateBranchName, title, description string, rec *report.Recorder) error {
	err := repository.Push(&git.PushOptions{
		RemoteName: "origin",
		RefSpecs: []gitConfig.RefSpec{
			gitConfig.RefSpec(fmt.Sprintf("refs/heads/%s:refs/heads/%s", updateBranchName, updateBranchName)),
		},
		Auth: repo.BasicAuth(ws.config.Token),
	})

	if err != nil {
		return fmt.Errorf("failed to push changes: %w", err)
	}

	mr, err := ws.platform.CreateMergeRequest(ctx, title, description, updateBranchName, ws.config.Branch)
	if err != nil {
		if deleteErr := ws.platform.DeleteBranch(ctx, updateBranchName); deleteErr != nil {
			ws.logger.Warn("failed to delete remote branch after MR creation failure",
				zap.String("branch", updateBranchName),
				zap.Error(deleteErr),
			)
		}
		return fmt.Errorf("failed to create merge request: %w", err)
	}
	ws.logger.Info("merge request created", zap.String("url", mr.URL))
	rec.SetMergeRequest(mr.URL)

	// Best-effort: the MR already exists, so failing here would redden a perfectly good job.
	// Recorded either way, or the report shows a clean success for an MR that will never merge.
	if ws.config.ActiveRunType().AutoMerge {
		err := ws.platform.EnableAutoMerge(ctx, mr)
		rec.SetAutoMerge(err)
		if err != nil {
			ws.logger.Warn("failed to enable auto merge", zap.String("url", mr.URL), zap.Error(err))
		} else {
			ws.logger.Info("auto merge enabled", zap.String("url", mr.URL))
		}
	}

	return nil
}

// descriptionTemplates parses the embedded templates once: the FS is compiled in, result fixed.
var descriptionTemplates = sync.OnceValues(func() (*template.Template, error) {
	return template.ParseFS(templates, "templates/*.go.tmpl")
})

func (ws *WorkflowBaseService) GenerateDescription(data any, filename string) (string, error) {
	tmpl, err := descriptionTemplates()
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
