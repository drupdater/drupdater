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
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/drupdater/drupdater/internal"
	"github.com/drupdater/drupdater/internal/report"
	"github.com/drupdater/drupdater/pkg/composer"

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

	// siteCommitMu serializes the git-committing tail of updateSite. Sites are updated
	// concurrently, but they share one working tree and git index, and drush config:export
	// commits via git itself, so the staging/commit steps must not overlap.
	siteCommitMu sync.Mutex

	// reportSink receives the run report when the run ends, on every exit path. nil when
	// --report was not given, in which case no report is assembled beyond the in-memory
	// bookkeeping the recorder does anyway.
	reportSink func(report.Report)
}

// Option configures a WorkflowBaseService. Options are variadic so adding one does not disturb
// existing call sites.
type Option func(*WorkflowBaseService)

// WithReportSink makes the run hand its finished report to sink. The sink is called exactly
// once per run, after every other deferred cleanup has had its say, including when the run
// fails.
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

	// Emit the report from its own defer, registered before anything that can fail, so that
	// "written on every exit path" holds literally — including a clone or checkout that never
	// gets far enough for there to be a working copy to clean up. Being registered first also
	// makes it run last, so the report describes the run after every other teardown has had its
	// say.
	//
	// An AbortError is not a failure — it is the workflow's way of saying there was nothing to
	// do — so it is recorded as such rather than as an error the reader should act on.
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

	// platform is nil for a checkout-mode --dry-run run with no token (see cmd/root.go's
	// tokenRequired): that run never pushes or creates an MR, so no VCS client was built and
	// the checkout's own git identity (already configured locally, e.g. by CI) is used as-is.
	var username, email string
	if ws.platform != nil {
		username, email = ws.platform.GetUser(ctx)
	}

	// Acquire a single working directory: the existing checkout (default, CI) or a fresh
	// clone (--clone, for local testing). Old and new code live in this one directory
	// sequentially: install the baseline site, then composer update, then run update hooks.
	//
	// Recorded as a phase like every other step: a bad token, an unreachable host or an
	// unreadable checkout fails here, and that is one of the most common ways a run fails in
	// practice — exactly what the report exists to name.
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

	// Capture the checkout's current HEAD before any branch is created, so a failed or aborted
	// run can restore it below — see captureOriginalHead.
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

// runPhases executes the update itself, one recorded phase at a time. It is separate from
// StartUpdate so that the run's setup and teardown (timeout, working copy, checkout restore,
// report emission) stay readable next to each other rather than being buried under the phases.
func (ws *WorkflowBaseService) runPhases(
	ctx context.Context,
	rec *report.Recorder,
	repository GitRepository,
	worktree Worktree,
	path string,
	addons []internal.Addon,
) error {
	// Fail fast on cheap, structural prerequisites shared with "drupdater check" instead of
	// discovering them mid-run: a shallow checkout only fails much later, with a cryptic
	// "object not found" when the update branch is pushed. Extension requirements are
	// deliberately not checked here (see CheckPlatformReqs).
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

	// Assemble the merge request's title and description. This is a phase of its own, ahead of
	// publish, because it has to happen under --dry-run as well: the description is assembled
	// from every addon's RenderTemplate and is the run's only human-readable account of itself.
	// Rendered only when publishing, a broken template would be invisible to every dry run and
	// would surface in a real run only after the branch had already been pushed.
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

// renderMergeRequest produces the title and description for the run's merge request.
//
// The title starts as the maintenance-update default and is offered to the addons through
// pre-merge-request-create, which is how a security run gets re-labelled by composer_audit. The
// event fires here rather than in publishWork so that the title a dry run reports is the one a
// real run would have used.
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

// captureOriginalHead returns the checkout's current HEAD in checkout mode, or nil in clone mode
// (a clone's whole temp directory is discarded by cleanup regardless, so there is nothing to
// restore there) or if HEAD could not be read. It is used by StartUpdate to put a checkout-mode
// run's working directory back exactly where it found it if the run does not complete
// successfully — otherwise a failed or aborted run leaves the checkout on drupdater's own
// throwaway work branch, with composer.json's allow-plugins possibly still left at true by
// composer_allow_plugins' own pre-composer-update handler and never reverted. Best-effort: a
// failure to read HEAD only means the restore is skipped, not that the run itself aborts over
// what is otherwise a safety net.
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

// restoreOriginalCheckout switches worktree back to originalRef, discarding any uncommitted
// changes and stray commits accumulated on drupdater's own throwaway work branch. It runs only
// to tidy up after a run that has already failed or been aborted, so an error here is logged,
// not returned: it must never mask the original failure.
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

// forEachSite runs fn for every configured site concurrently, bounded by config.Concurrency
// (or GOMAXPROCS(0), which reflects the container's CPU quota, when unset), and cancels the
// rest on the first error.
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

// cleanup removes the artifacts the run created. In clone mode that's this run's own temp
// clone; in checkout mode it's the SQLite databases and private files written beside the
// checkout.
func (ws *WorkflowBaseService) cleanup(path string) {
	if ws.config.Clone {
		// Remove only this run's clone directory, not the shared per-URL parent, so a
		// concurrent run of the same repository isn't wiped out. The clone always lives in a
		// unique sub-directory of the temp dir; guard against removing the temp dir itself.
		if rel, err := filepath.Rel(os.TempDir(), path); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
			os.RemoveAll(path)
		}
		return
	}
	parent := filepath.Dir(path)
	for _, site := range ws.config.Sites {
		os.Remove(filepath.Join(parent, site+".sqlite"))
		// Remove only the per-site directory the installer created, never the whole "private"
		// tree: a private files directory next to the checkout is a standard Drupal layout, so
		// that parent can hold real project data this run does not own.
		os.RemoveAll(filepath.Join(parent, "private", site))
	}
	// Drop the parent too, but only if removing the per-site directories left it empty —
	// os.Remove refuses a non-empty directory, which is exactly the guard we want here.
	os.Remove(filepath.Join(parent, "private"))
}

func (ws *WorkflowBaseService) updateSharedCode(ctx context.Context, repository GitRepository, worktree Worktree, path string, rec *report.Recorder) (string, error) {
	ws.logger.Info("updating dependencies")

	// Do all of the update work on a dedicated branch. In checkout mode the run operates on the
	// branch the user (or CI) has checked out, and the addons commit as they go; without this,
	// an abort ("branch already exists") or a mid-run failure would leave those commits — and a
	// modified composer.json — on that branch. The final, hash-named branch is branched from
	// this one once the composer.lock hash is known.
	// Flat name, not "drupdater/work-<ts>": a target repo with a branch literally named
	// "drupdater" would make refs/heads/drupdater a file, blocking any nested drupdater/* ref.
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
//
// The local check runs first: a local ref by this name can be left over from a prior
// checkout-mode run of the same code-content hash that got this far before failing (see
// restoreOriginalCheckout — it puts the checkout's HEAD back, but never deletes local branches
// drupdater created). Without it, the Create:true checkout that follows this call would fail on
// go-git's raw "a branch named ... already exists" instead of the same clean AbortError a remote
// collision gets.
func (ws *WorkflowBaseService) ensureUpdateBranchAvailable(repository GitRepository, updateBranchName string) error {
	if _, err := repository.Reference(plumbing.NewBranchReferenceName(updateBranchName), false); err == nil {
		return AbortError{Msg: fmt.Sprintf("branch %s already exists locally, skipping", updateBranchName)}
	} else if !errors.Is(err, plumbing.ErrReferenceNotFound) {
		return fmt.Errorf("failed to check for a local %s branch: %w", updateBranchName, err)
	}

	// The remote half only matters when the branch is going to be pushed. A --dry-run never
	// pushes, and reaching the remote here would break the one case that is documented as
	// needing no token at all: a checkout-mode dry run. Worse than needing one, it fails
	// outright without it — go-git sends an empty password rather than no credential, which
	// the host rejects even for a public repository.
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

	// The remaining steps stage and commit into the shared working tree (the post-site-update
	// addons via go-git, and drush config:export via git directly), so run them one site at a
	// time even though the sites themselves are updated concurrently.
	return ws.commitSiteChanges(ctx, path, worktree, site)
}

// commitSiteChanges runs the post-site-update event and configuration export under a lock so
// the git operations of concurrently-updated sites don't race on the shared index.
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

// toReportPackages converts composer's package changes into the report's own schema type. The
// two are kept separate deliberately — see report.PackageChange.
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
		Auth: &http.BasicAuth{
			Username: "du", // yes, this can be anything except an empty string
			Password: ws.config.Token,
		},
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

	// Auto-merge is a convenience on top of work that has already succeeded: the branch is
	// pushed and the MR exists. Failing the run here (repository forbids auto-merge, token
	// lacks the scope, platform hiccup) would report a red job for a perfectly good MR, so
	// the error is logged and the run still succeeds. The MR is deliberately left in place.
	//
	// It is recorded either way, because a warning in the log is easy to miss and the report
	// would otherwise show a clean success for a run whose MR will never merge itself.
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
