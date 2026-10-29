package workflow

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

type Runner struct {
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

// Option configures a Runner. Variadic so adding one does not disturb call sites.
type Option func(*Runner)

// WithReportSink hands the finished report to sink once per run, after every other cleanup.
func WithReportSink(sink func(report.Report)) Option {
	return func(r *Runner) {
		r.reportSink = sink
	}
}

func New(
	logger *zap.Logger,
	config internal.Config,
	drush Drush,
	platform Platform,
	repository Repository,
	installer Installer,
	composerService Composer,
	dispatcher EventDispatcher,
	opts ...Option,
) *Runner {
	r := &Runner{
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
		opt(r)
	}

	return r
}

func (r *Runner) Run(ctx context.Context, addons []Addon) (err error) {
	start := time.Now()

	mode := report.ModeNormal
	if r.config.Security {
		mode = report.ModeSecurity
	}
	rec := report.NewRecorder(internal.Version, mode, r.config.DryRun, r.config.RepositoryURL, r.config.Branch, r.config.Sites)

	// Registered first, so it runs last and covers every exit path. An AbortError means there
	// was nothing to do, not that the run failed.
	defer func() {
		if r.reportSink == nil {
			return
		}
		if _, ok := errors.AsType[AbortError](err); ok {
			rec.SetNoChanges()
		}
		for _, a := range addons {
			rec.AddReporter(a)
		}
		r.reportSink(rec.Finish())
	}()

	// Bound the whole run so a wedged subprocess or network call can't hang forever.
	if r.config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.config.Timeout)
		defer cancel()
	}

	// After the timeout, so the lookup is bounded too. Not a phase: it cannot fail the run.
	rec.SetToolVersions(LookupToolVersions(ctx, r.logger, r.composer))

	// platform is nil for a token-free checkout-mode dry run (see tokenRequired), which keeps
	// the checkout's own git identity.
	var username, email string
	if r.platform != nil {
		username, email = r.platform.GetUser(ctx)
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
		repository, worktree, path, acquireErr = r.acquireWorkingCopy(username, email)
		return acquireErr
	}); err != nil {
		return err
	}

	// Before any branch is created, so a failed run can be put back — see captureOriginalHead.
	originalRef := r.captureOriginalHead(repository)

	defer func() {
		r.logger.Info("update run finished", zap.Duration("duration", time.Since(start)))
		if err != nil && originalRef != nil {
			r.restoreOriginalCheckout(worktree, originalRef)
		}
		r.cleanup(path)
	}()

	return r.runPhases(ctx, rec, repository, worktree, path, addons)
}

// runPhases executes the update, one recorded phase at a time. Separate from Run so the
// run's setup and teardown stay readable next to each other.
func (r *Runner) runPhases(
	ctx context.Context,
	rec *report.Recorder,
	repository GitRepository,
	worktree Worktree,
	path string,
	addons []Addon,
) error {
	// Fail fast on the prerequisites "drupdater check" shares. Extension requirements are
	// deliberately not among them — see CheckPlatformReqs.
	if err := rec.Run("preflight", func() error {
		if result := CheckGitHistoryComplete(r.repository, path); !result.OK {
			return fmt.Errorf("%s: %s", result.Name, result.Detail)
		}
		if result := CheckPlatformRequirements(ctx, r.composer, path); !result.OK {
			return fmt.Errorf("PHP platform requirements not satisfied:\n%s", result.Detail)
		}
		return nil
	}); err != nil {
		return err
	}

	if err := rec.Run("composer install", func() error {
		r.logger.Info("running composer install")
		if err := r.composer.Install(ctx, path); err != nil {
			return fmt.Errorf("failed to run composer install: %w", err)
		}
		return nil
	}); err != nil {
		return err
	}

	// Install each site at the current (old) code to create the baseline database.
	if err := rec.Run("baseline site install", func() error {
		return r.forEachSite(ctx, func(ctx context.Context, site string) error {
			if err := r.installer.Install(ctx, path, site); err != nil {
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
		updateBranchName, err = r.updateSharedCode(ctx, repository, worktree, path, rec)
		return err
	}); err != nil {
		return err
	}
	rec.SetUpdateBranch(updateBranchName)

	// Run the update hooks and export config per site against the now-updated code.
	if err := rec.Run("site update", func() error {
		return r.forEachSite(ctx, func(ctx context.Context, site string) error {
			return r.updateSite(ctx, path, worktree, site)
		})
	}); err != nil {
		return err
	}

	// Ahead of publish so it runs under --dry-run too: rendered later, a broken template would
	// only surface after the branch had been pushed.
	var mrTitle, mrDescription string
	if err := rec.Run("render merge request", func() error {
		var renderErr error
		mrTitle, mrDescription, renderErr = r.renderMergeRequest(addons)
		return renderErr
	}); err != nil {
		return err
	}
	rec.SetMergeRequestContent(mrTitle, mrDescription)

	if !r.config.DryRun {
		return rec.Run("publish", func() error {
			return r.publishWork(ctx, repository, updateBranchName, mrTitle, mrDescription, rec)
		})
	}
	return nil
}

// renderMergeRequest produces the title and description. The title starts as the maintenance
// default and is offered to the addons — how composer_audit re-labels a security run.
func (r *Runner) renderMergeRequest(addons []Addon) (string, string, error) {
	e := NewPreMergeRequestCreateEvent(fmt.Sprintf("%s: Drupal Maintenance Updates", r.current.Format("January 2006")))
	if err := r.dispatcher.FireEvent(e); err != nil {
		return "", "", fmt.Errorf("failed to fire event: %w", err)
	}

	description, err := renderDescription(addons)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate description: %w", err)
	}

	return e.Title, description, nil
}

// captureOriginalHead returns the checkout's HEAD so a failed run can be put back rather than
// left on drupdater's throwaway work branch. A safety net: nil in clone mode or on failure.
func (r *Runner) captureOriginalHead(repository GitRepository) *plumbing.Reference {
	if r.config.Clone {
		return nil
	}
	ref, err := repository.Head()
	if err != nil {
		r.logger.Warn("failed to determine current HEAD, checkout will not be restored if the run fails", zap.Error(err))
		return nil
	}
	return ref
}

// restoreOriginalCheckout switches worktree back to originalRef, discarding whatever the work
// branch accumulated. Errors are logged, never returned: this must not mask the real failure.
func (r *Runner) restoreOriginalCheckout(worktree Worktree, originalRef *plumbing.Reference) {
	opts := &git.CheckoutOptions{Force: true}
	if originalRef.Name().IsBranch() {
		opts.Branch = originalRef.Name()
	} else {
		opts.Hash = originalRef.Hash()
	}
	if err := worktree.Checkout(opts); err != nil {
		r.logger.Warn("failed to restore checkout to its original state", zap.Error(err))
	}
}

// acquireWorkingCopy returns the single working directory the run operates on. By default it
// opens the existing checkout in place; with --clone it clones the repository to a temp dir.
func (r *Runner) acquireWorkingCopy(username, email string) (GitRepository, Worktree, string, error) {
	if r.config.Clone {
		r.logger.Info("cloning repository", zap.String("url", r.config.RepositoryURL), zap.String("branch", r.config.Branch))
		return r.repository.CloneRepository(r.config.RepositoryURL, r.config.Branch, r.config.Token, username, email)
	}
	return r.repository.OpenRepository(r.config.WorkingDir, username, email)
}

// stageScaffoldChanges stages the web-root files drupal-scaffold rewrites on a core update --
// .htaccess, robots.txt, index.php -- which the composer.* glob does not cover.
//
// Tracked paths only: vendor/ and web/core legitimately sit untracked in the checkout.
func (r *Runner) stageScaffoldChanges(ctx context.Context, path string, worktree Worktree) error {
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
	webroot, err := composer.WebRoot(ctx, r.composer, path)
	if err != nil {
		return fmt.Errorf("failed to determine web root: %w", err)
	}
	staged := make([]string, 0, len(candidates))
	for _, file := range candidates {
		// Web root only: everything outside it belongs to a later phase, and sweeping it in
		// here would commit it mid-update, still carrying the baseline install's state.
		if !strings.HasPrefix(file, webroot+"/") {
			continue
		}
		if _, err := worktree.Add(file); err != nil {
			return fmt.Errorf("failed to stage %s: %w", file, err)
		}
		staged = append(staged, file)
	}
	if len(staged) > 0 {
		r.logger.Info("staged scaffold changes", zap.Strings("files", staged))
	}
	return nil
}

// forEachSite runs fn per site concurrently, bounded by config.Concurrency, cancelling the rest
// on the first error.
func (r *Runner) forEachSite(ctx context.Context, fn func(context.Context, string) error) error {
	g, groupCtx := errgroup.WithContext(ctx)
	limit := r.config.Concurrency
	if limit <= 0 {
		limit = runtime.GOMAXPROCS(0)
	}
	g.SetLimit(limit)
	for _, site := range r.config.Sites {
		g.Go(func() error {
			return fn(groupCtx, site)
		})
	}
	return g.Wait()
}

// cleanup removes a clone-mode run's temp clone. Site databases live in the installer's own
// directory, which its owner removes.
func (r *Runner) cleanup(path string) {
	if r.config.Clone {
		// This run's own directory, not the shared per-URL parent, so a concurrent run of the
		// same repository isn't wiped out. The Rel guard keeps the temp dir itself safe.
		if rel, err := filepath.Rel(os.TempDir(), path); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
			os.RemoveAll(path)
		}
	}
}

func (r *Runner) updateSharedCode(ctx context.Context, repository GitRepository, worktree Worktree, path string, rec *report.Recorder) (string, error) {
	r.logger.Info("updating dependencies")

	// A dedicated branch: the addons commit as they go, and a mid-run failure would otherwise
	// strand those commits on the user's own branch. Flat name, not "drupdater/work-<ts>":
	// a branch named "drupdater" makes refs/heads/drupdater a file, blocking nested refs.
	workBranch := fmt.Sprintf("drupdater-work-%d", r.current.UnixNano())
	if err := worktree.Checkout(&git.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName(workBranch),
		Create: true,
		Keep:   true,
	}); err != nil {
		return "", fmt.Errorf("failed to create work branch: %w", err)
	}

	preComposerUpdateEvent := NewPreComposerUpdateEvent(ctx, path, worktree, []string{}, []string{}, false)
	if err := r.dispatcher.FireEvent(preComposerUpdateEvent); err != nil {
		return "", fmt.Errorf("failed to fire event: %w", err)
	}

	changes, err := r.composer.Update(ctx, path, preComposerUpdateEvent.PackagesToUpdate, preComposerUpdateEvent.PackagesToKeep, preComposerUpdateEvent.MinimalChanges, false)
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
	r.logger.Info("dependencies updated",
		zap.Int("total", len(changes)),
		zap.Int("installed", byAction["Install"]),
		zap.Int("upgraded", byAction["Upgrade"]),
		zap.Int("downgraded", byAction["Downgrade"]),
		zap.Int("removed", byAction["Remove"]),
	)

	postComposerUpdateEvent := NewPostComposerUpdateEvent(ctx, path, worktree)
	if err := r.dispatcher.FireEvent(postComposerUpdateEvent); err != nil {
		return "", fmt.Errorf("failed to fire event: %w", err)
	}

	if err := worktree.AddGlob("composer.*"); err != nil {
		return "", fmt.Errorf("failed to add composer.* files: %w", err)
	}
	if err := r.stageScaffoldChanges(ctx, path, worktree); err != nil {
		return "", err
	}
	if _, err := worktree.Commit("Update composer.json and composer.lock", &git.CommitOptions{}); err != nil {
		return "", fmt.Errorf("failed to commit composer.json and composer.lock: %w", err)
	}

	postCodeUpdateEvent := NewPostCodeUpdateEvent(ctx, path, worktree)
	if err := r.dispatcher.FireEvent(postCodeUpdateEvent); err != nil {
		return "", fmt.Errorf("failed to fire event: %w", err)
	}

	composerLockHash, err := r.composer.GetLockHash(path)
	if err != nil {
		return "", err
	}

	updateBranchName := fmt.Sprintf("update-%s", composerLockHash)

	if err := r.ensureUpdateBranchAvailable(repository, updateBranchName); err != nil {
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
func (r *Runner) ensureUpdateBranchAvailable(repository GitRepository, updateBranchName string) error {
	if _, err := repository.Reference(plumbing.NewBranchReferenceName(updateBranchName), false); err == nil {
		return AbortError{Msg: fmt.Sprintf("branch %s already exists locally, skipping", updateBranchName)}
	} else if !errors.Is(err, plumbing.ErrReferenceNotFound) {
		return fmt.Errorf("failed to check for a local %s branch: %w", updateBranchName, err)
	}

	// Skipped for a dry run: go-git sends an empty password rather than no credential, which
	// hosts reject even for a public repository — breaking the token-free case.
	if r.config.DryRun {
		return nil
	}

	exists, err := r.repository.BranchExists(repository, updateBranchName, r.config.Token)
	if err != nil {
		return fmt.Errorf("failed to check if branch exists: %w", err)
	}
	if exists {
		return AbortError{Msg: fmt.Sprintf("branch %s already exists, skipping", updateBranchName)}
	}
	return nil
}

func (r *Runner) updateSite(ctx context.Context, path string, worktree Worktree, site string) (err error) {
	r.logger.Info("updating site", zap.String("site", site))

	if err := r.installer.ConfigureDatabase(ctx, path, site); err != nil {
		return fmt.Errorf("failed to configure database: %w", err)
	}
	// Uncancelled: the first failing site cancels ctx, and the rest must still be put back.
	defer func() {
		if restoreErr := r.installer.RestoreSettings(context.WithoutCancel(ctx), path, site); restoreErr != nil {
			err = errors.Join(err, fmt.Errorf("failed to restore settings: %w", restoreErr))
		}
	}()

	preSiteUpdateEvent := NewPreSiteUpdateEvent(ctx, path, worktree, site)
	if err := r.dispatcher.FireEvent(preSiteUpdateEvent); err != nil {
		return fmt.Errorf("failed to fire event: %w", err)
	}

	if err := r.drush.UpdateSite(ctx, path, site); err != nil {
		return fmt.Errorf("failed to update site: %w", err)

	}

	if err := r.drush.ConfigResave(ctx, path, site); err != nil {
		return fmt.Errorf("failed to resave config: %w", err)

	}

	// The remaining steps commit into the shared working tree, so one site at a time.
	return r.commitSiteChanges(ctx, path, worktree, site)
}

// commitSiteChanges holds a lock so concurrently-updated sites don't race on the shared index.
func (r *Runner) commitSiteChanges(ctx context.Context, path string, worktree Worktree, site string) error {
	r.siteCommitMu.Lock()
	defer r.siteCommitMu.Unlock()

	postSiteUpdateEvent := NewPostSiteUpdateEvent(ctx, path, worktree, site)
	if err := r.dispatcher.FireEvent(postSiteUpdateEvent); err != nil {
		return fmt.Errorf("failed to fire event: %w", err)
	}

	r.logger.Info("exporting configuration", zap.String("site", site))
	if err := r.drush.ExportConfiguration(ctx, path, site); err != nil {
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

func (r *Runner) publishWork(ctx context.Context, repository GitRepository, updateBranchName, title, description string, rec *report.Recorder) error {
	err := repository.Push(&git.PushOptions{
		RemoteName: "origin",
		RefSpecs: []gitConfig.RefSpec{
			gitConfig.RefSpec(fmt.Sprintf("refs/heads/%s:refs/heads/%s", updateBranchName, updateBranchName)),
		},
		Auth: repo.BasicAuth(r.config.Token),
	})

	if err != nil {
		return fmt.Errorf("failed to push changes: %w", err)
	}

	mr, err := r.platform.CreateMergeRequest(ctx, title, description, updateBranchName, r.config.Branch)
	if err != nil {
		if deleteErr := r.platform.DeleteBranch(ctx, updateBranchName); deleteErr != nil {
			r.logger.Warn("failed to delete remote branch after MR creation failure",
				zap.String("branch", updateBranchName),
				zap.Error(deleteErr),
			)
		}
		return fmt.Errorf("failed to create merge request: %w", err)
	}
	r.logger.Info("merge request created", zap.String("url", mr.URL))
	rec.SetMergeRequest(mr.URL)

	// Best-effort: the MR already exists, so failing here would redden a perfectly good job.
	// Recorded either way, or the report shows a clean success for an MR that will never merge.
	if r.config.ActiveRunType().AutoMerge {
		err := r.platform.EnableAutoMerge(ctx, mr)
		rec.SetAutoMerge(err)
		if err != nil {
			r.logger.Warn("failed to enable auto merge", zap.String("url", mr.URL), zap.Error(err))
		} else {
			r.logger.Info("auto merge enabled", zap.String("url", mr.URL))
		}
	}

	return nil
}

// descriptionTemplates parses the embedded templates once: the FS is compiled in, result fixed.
var descriptionTemplates = sync.OnceValues(func() (*template.Template, error) {
	return template.ParseFS(templates, "templates/*.go.tmpl")
})

func renderDescription(addons []Addon) (string, error) {
	tmpl, err := descriptionTemplates()
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %w", err)
	}

	var output bytes.Buffer
	if err := tmpl.ExecuteTemplate(&output, "dependency_update.go.tmpl", struct{ Addons []Addon }{addons}); err != nil {
		return "", fmt.Errorf("failed to execute template: %w", err)
	}

	return output.String(), nil
}
