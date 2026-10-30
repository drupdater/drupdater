package workflow

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/drupdater/drupdater/internal"
	"github.com/drupdater/drupdater/internal/mergerequest"
	"github.com/drupdater/drupdater/internal/report"
	"github.com/drupdater/drupdater/pkg/composer"
	"github.com/drupdater/drupdater/pkg/repo"

	git "github.com/go-git/go-git/v5"
	gitConfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

// Runner updates a project. It holds only what all its runs share, so it can run again.
type Runner struct {
	services

	// reportSink receives the run report on every exit path. nil when --report was not given.
	reportSink func(report.Report)
}

// services are a Runner's dependencies, shared read-only by all its runs.
type services struct {
	logger   *zap.Logger
	config   internal.Config
	drush    Drush
	platform Platform
	// git works on repositories in general; each run holds its own GitRepository.
	git       GitService
	installer Installer
	composer  Composer
}

// run is one call to Run from its working copy on. Everything in it is fixed by newRun.
type run struct {
	services
	hooks  *Pipeline
	addons []Addon
	rec    *report.Recorder

	repository GitRepository
	worktree   Worktree
	path       string
	// author signs the run's own commits, as committer signs the addons'.
	author    repo.Identity
	committer Committer
}

func newRun(s services, checkout repo.Checkout, addons []Addon, rec *report.Recorder) *run {
	return &run{
		services:   s,
		hooks:      NewPipeline(addons),
		addons:     addons,
		rec:        rec,
		repository: checkout.Repository,
		worktree:   checkout.Worktree,
		path:       checkout.Path,
		author:     checkout.Author,
		committer:  newGitCommitter(checkout.Worktree, checkout.Author),
	}
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
	gitService GitService,
	installer Installer,
	composerService Composer,
	opts ...Option,
) *Runner {
	r := &Runner{services: services{
		logger:    logger,
		config:    config,
		drush:     drush,
		platform:  platform,
		git:       gitService,
		installer: installer,
		composer:  composerService,
	}}
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
	var checkout repo.Checkout
	if err = rec.Run("acquire working copy", func() error {
		var acquireErr error
		checkout, acquireErr = r.acquireWorkingCopy(ctx, username, email)
		return acquireErr
	}); err != nil {
		return err
	}

	defer func() {
		r.logger.Info("update run finished", zap.Duration("duration", time.Since(start)))
		r.cleanup(context.WithoutCancel(ctx), checkout.Path)
	}()

	branch, err := newRun(r.services, checkout, addons, rec).execute(ctx)
	if err != nil {
		r.removeUpdateBranch(checkout.Repository, branch)
	}
	return err
}

// execute runs the phases, returning the update branch it created ("" if none) with any error.
func (r *run) execute(ctx context.Context) (branch string, err error) {
	// Fail fast on the prerequisites "drupdater check" shares. Extension requirements are
	// deliberately not among them — see CheckPlatformReqs.
	if err := r.rec.Run("preflight", func() error {
		if result := CheckGitHistoryComplete(r.git, r.path); !result.OK {
			return fmt.Errorf("%s: %s", result.Name, result.Detail)
		}
		if result := CheckPlatformRequirements(ctx, r.composer, r.path); !result.OK {
			return fmt.Errorf("PHP platform requirements not satisfied:\n%s", result.Detail)
		}
		return nil
	}); err != nil {
		return "", err
	}

	if err := r.rec.Run("composer install", func() error {
		r.logger.Info("running composer install")
		if err := r.composer.Install(ctx, r.path); err != nil {
			return fmt.Errorf("failed to run composer install: %w", err)
		}
		return nil
	}); err != nil {
		return "", err
	}

	// Ahead of the baseline install, so a run with nothing to do stops before installing any site.
	var updateBranchName string
	if err := r.rec.Run("resolve update", func() error {
		var err error
		updateBranchName, err = r.resolveUpdate(ctx)
		return err
	}); err != nil {
		return "", err
	}

	// Install each site at the current (old) code to create the baseline database.
	if err := r.rec.Run("baseline site install", func() error {
		return r.forEachSite(ctx, func(ctx context.Context, _ int, site string) error {
			if err := r.installer.Install(ctx, r.path, site); err != nil {
				return fmt.Errorf("site %s installation failed: %w", site, err)
			}
			return nil
		})
	}); err != nil {
		return "", err
	}

	// Update the shared code: install the resolved update and commit it.
	if err := r.rec.Run("update shared code", func() error {
		return r.updateSharedCode(ctx)
	}); err != nil {
		return "", err
	}
	r.rec.SetUpdateBranch(updateBranchName)

	// Run the update hooks and export config per site against the now-updated code, then point the
	// update branch at the result.
	if err := r.rec.Run("site update", func() error {
		if err := r.updateSites(ctx); err != nil {
			return err
		}
		if err := r.createUpdateBranch(updateBranchName); err != nil {
			return err
		}
		branch = updateBranchName
		return nil
	}); err != nil {
		return "", err
	}

	// Ahead of publish so it runs under --dry-run too: rendered later, a broken template would
	// only surface after the branch had been pushed.
	var mrTitle, mrDescription string
	if err := r.rec.Run("render merge request", func() error {
		var renderErr error
		mrTitle, mrDescription, renderErr = r.renderMergeRequest()
		return renderErr
	}); err != nil {
		return branch, err
	}
	r.rec.SetMergeRequestContent(mrTitle, mrDescription)

	if r.config.DryRun {
		// Equivalent mutant: Run reads the branch only on failure, and this is success.
		// mutator-disable-next-line statement/return
		return branch, nil
	}
	return branch, r.rec.Run("publish", func() error {
		return r.publish(ctx, branch, mrTitle, mrDescription)
	})
}

// renderMergeRequest renders from the report as it stands, so the merge request says what the
// report says. Every addon has finished by now; the final report re-reads the same sections.
func (r *run) renderMergeRequest() (string, string, error) {
	for _, a := range r.addons {
		r.rec.AddReporter(a)
	}
	snapshot := r.rec.Snapshot()

	description, err := mergerequest.Description(snapshot)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate description: %w", err)
	}

	return mergerequest.Title(snapshot), description, nil
}

// acquireWorkingCopy clones in clone mode. In checkout mode it adds a linked worktree of the
// checkout, so the run never changes the user's own working tree, branch or git config.
func (s services) acquireWorkingCopy(ctx context.Context, username, email string) (repo.Checkout, error) {
	if s.config.Clone {
		s.logger.Info("cloning repository", zap.String("url", s.config.RepositoryURL), zap.String("branch", s.config.Branch))
		return s.git.CloneRepository(s.config.RepositoryURL, s.config.Branch, s.config.Token, username, email)
	}
	return s.git.AddWorktree(ctx, s.config.WorkingDir, username, email)
}

// stageScaffoldChanges stages the web-root files drupal-scaffold rewrites on a core update --
// .htaccess, robots.txt, index.php -- which the composer.* glob does not cover.
//
// Tracked paths only: vendor/ and web/core legitimately sit untracked in the checkout.
func (r *run) stageScaffoldChanges(ctx context.Context) error {
	status, err := r.worktree.Status()
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
	webroot, err := composer.WebRoot(ctx, r.composer, r.path)
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
		if _, err := r.worktree.Add(file); err != nil {
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
// on the first error. Sites start in config order, which updateSite's turns rely on.
func (s services) forEachSite(ctx context.Context, fn func(context.Context, int, string) error) error {
	g, groupCtx := errgroup.WithContext(ctx)
	limit := s.config.Concurrency
	if limit <= 0 {
		limit = runtime.GOMAXPROCS(0)
	}
	g.SetLimit(limit)
	for i, site := range s.config.Sites {
		g.Go(func() error {
			return fn(groupCtx, i, site)
		})
	}
	return g.Wait()
}

// cleanup removes the run's working copy, whichever mode made it. Errors only warn: a run's
// outcome is already decided, and a leftover temporary directory must not change it.
func (s services) cleanup(ctx context.Context, path string) {
	if !s.config.Clone {
		if err := s.git.RemoveWorktree(ctx, s.config.WorkingDir, path); err != nil {
			s.logger.Warn("failed to remove the run's worktree", zap.String("path", path), zap.Error(err))
		}
		return
	}
	// This run's own directory, not the shared per-URL parent, so a concurrent run of the
	// same repository isn't wiped out. The Rel guard keeps the temp dir itself safe.
	if rel, err := filepath.Rel(os.TempDir(), path); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
		os.RemoveAll(path)
	}
}

// removeUpdateBranch deletes the update branch a failed run created, or the next run would find
// it and stop as if there were nothing to do. "" means the run created none.
func (s services) removeUpdateBranch(repository GitRepository, branch string) {
	if branch == "" {
		return
	}
	if err := repository.RemoveBranch(branch); err != nil {
		s.logger.Warn("failed to remove the failed run's update branch", zap.String("branch", branch), zap.Error(err))
	}
}

// resolveUpdate settles what the update changes and what its branch is called, touching only
// composer.json and composer.lock: vendor/ keeps the old code for the baseline install.
func (r *run) resolveUpdate(ctx context.Context) (string, error) {
	r.logger.Info("resolving dependency updates")

	plan, err := Settle(r.hooks, NewPreComposerUpdateEvent(ctx, r.path, r.committer, UpdatePlan{}))
	if err != nil {
		return "", err
	}

	changes, err := r.composer.Update(ctx, r.path, plan.Only, plan.KeepConstraints(), plan.MinimalChanges, false)
	if err != nil {
		return "", fmt.Errorf("failed to update dependencies: %w", err)
	}
	r.rec.SetPackages(toReportPackages(changes))
	if len(changes) == 0 {
		return "", AbortError{Msg: "no changes detected"}
	}

	byAction := map[string]int{}
	for _, c := range changes {
		byAction[c.Action]++
	}
	r.logger.Info("dependencies resolved",
		zap.Int("total", len(changes)),
		zap.Int("installed", byAction["Install"]),
		zap.Int("upgraded", byAction["Upgrade"]),
		zap.Int("downgraded", byAction["Downgrade"]),
		zap.Int("removed", byAction["Remove"]),
	)

	// Named from the lock as resolved, before any addon rewrites composer.json, so the name can
	// be checked here rather than after the site installs.
	composerLockHash, err := r.composer.GetLockHash(r.path)
	if err != nil {
		return "", err
	}
	updateBranchName := fmt.Sprintf("update-%s", composerLockHash)

	if err := r.ensureUpdateBranchAvailable(updateBranchName); err != nil {
		return "", err
	}
	return updateBranchName, nil
}

// updateSharedCode brings vendor/ to the resolved lock and lets the addons work on the new code.
func (r *run) updateSharedCode(ctx context.Context) error {
	r.logger.Info("installing updated dependencies")
	if err := r.composer.Install(ctx, r.path); err != nil {
		return fmt.Errorf("failed to install updated dependencies: %w", err)
	}

	postComposerUpdateEvent := NewPostComposerUpdateEvent(ctx, r.path, r.committer)
	if err := Fire(r.hooks, postComposerUpdateEvent); err != nil {
		return err
	}

	if err := r.worktree.AddGlob("composer.*"); err != nil {
		return fmt.Errorf("failed to add composer.* files: %w", err)
	}
	if err := r.stageScaffoldChanges(ctx); err != nil {
		return err
	}
	if _, err := r.worktree.Commit("Update composer.json and composer.lock", commitOptions(r.author)); err != nil {
		return fmt.Errorf("failed to commit composer.json and composer.lock: %w", err)
	}

	postCodeUpdateEvent := NewPostCodeUpdateEvent(ctx, r.path, r.committer)
	if err := Fire(r.hooks, postCodeUpdateEvent); err != nil {
		return err
	}

	return nil
}

// createUpdateBranch points updateBranchName at HEAD. Only once the last commit is made: the run
// commits on a detached HEAD, which a branch created earlier would not follow.
func (r *run) createUpdateBranch(updateBranchName string) error {
	head, err := r.repository.Head()
	if err != nil {
		return fmt.Errorf("failed to read HEAD: %w", err)
	}
	if err := r.repository.SetBranch(updateBranchName, head.Hash()); err != nil {
		return fmt.Errorf("failed to create branch %s: %w", updateBranchName, err)
	}
	return nil
}

// ensureUpdateBranchAvailable returns an AbortError if updateBranchName is already taken, locally
// or on the remote, and a plain error if either check itself fails.
// The local check runs first: a prior failed run leaves its branch behind, and without it the
// checkout below fails on go-git's raw message instead of a clean AbortError.
func (r *run) ensureUpdateBranchAvailable(updateBranchName string) error {
	if _, err := r.repository.Reference(plumbing.NewBranchReferenceName(updateBranchName), false); err == nil {
		return AbortError{Msg: fmt.Sprintf("branch %s already exists locally, skipping", updateBranchName)}
	} else if !errors.Is(err, plumbing.ErrReferenceNotFound) {
		return fmt.Errorf("failed to check for a local %s branch: %w", updateBranchName, err)
	}

	// Skipped for a dry run: go-git sends an empty password rather than no credential, which
	// hosts reject even for a public repository — breaking the token-free case.
	if r.config.DryRun {
		return nil
	}

	exists, err := r.git.BranchExists(r.repository, updateBranchName, r.config.Token)
	if err != nil {
		return fmt.Errorf("failed to check if branch exists: %w", err)
	}
	if exists {
		return AbortError{Msg: fmt.Sprintf("branch %s already exists, skipping", updateBranchName)}
	}
	return nil
}

// updateSites runs updateSite for every site, handing each its turn: turns[i] closes once site i
// may write into the working tree, which happens only after every earlier site has.
func (r *run) updateSites(ctx context.Context) error {
	turns := make([]chan struct{}, len(r.config.Sites)+1)
	for i := range turns {
		turns[i] = make(chan struct{})
	}
	close(turns[0])
	return r.forEachSite(ctx, func(ctx context.Context, i int, site string) error {
		return r.updateSite(ctx, site, turns[i], turns[i+1])
	})
}

// updateSite updates the site's database concurrently with the other sites, then waits for its
// turn to write into the shared working tree: two sites may share a config or translation
// directory, and the commits land in --sites order however the sites finish.
func (r *run) updateSite(ctx context.Context, site string, turn <-chan struct{}, next chan<- struct{}) (err error) {
	r.logger.Info("updating site", zap.String("site", site))

	if err := r.installer.ConfigureDatabase(ctx, r.path, site); err != nil {
		return fmt.Errorf("failed to configure database: %w", err)
	}
	// Uncancelled: the first failing site cancels ctx, and the rest must still be put back.
	defer func() {
		if restoreErr := r.installer.RestoreSettings(context.WithoutCancel(ctx), r.path, site); restoreErr != nil {
			err = errors.Join(err, fmt.Errorf("failed to restore settings: %w", restoreErr))
		}
	}()

	preSiteUpdateEvent := NewPreSiteUpdateEvent(ctx, r.path, r.committer, site)
	if err := Fire(r.hooks, preSiteUpdateEvent); err != nil {
		return err
	}

	if err := r.drush.UpdateSite(ctx, r.path, site); err != nil {
		return fmt.Errorf("failed to update site: %w", err)

	}

	if err := r.drush.ConfigResave(ctx, r.path, site); err != nil {
		return fmt.Errorf("failed to resave config: %w", err)

	}

	select {
	case <-turn:
	case <-ctx.Done():
		return ctx.Err()
	}
	if err := r.commitSiteChanges(ctx, site); err != nil {
		return err
	}
	close(next)
	return nil
}

func (r *run) commitSiteChanges(ctx context.Context, site string) error {
	postSiteUpdateEvent := NewPostSiteUpdateEvent(ctx, r.path, r.committer, site)
	if err := Fire(r.hooks, postSiteUpdateEvent); err != nil {
		return err
	}

	r.logger.Info("exporting configuration", zap.String("site", site))
	if err := r.drush.ExportConfiguration(ctx, r.path, site); err != nil {
		return fmt.Errorf("failed to export configuration: %w", err)
	}
	configDir, err := r.drush.GetConfigSyncDir(ctx, r.path, site, true)
	if err != nil {
		return fmt.Errorf("failed to locate the exported configuration: %w", err)
	}
	// The committer reads an empty path as the whole tree, which would sweep in unrelated changes.
	if configDir == "" {
		return fmt.Errorf("failed to locate the exported configuration: site %s has no config sync directory", site)
	}
	if _, err := r.committer.Commit("Update configuration "+site, configDir); err != nil {
		return fmt.Errorf("failed to commit the exported configuration: %w", err)
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

func (r *run) publish(ctx context.Context, updateBranchName, title, description string) error {
	err := r.repository.Push(&git.PushOptions{
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
	r.rec.SetMergeRequest(mr.URL)

	// Best-effort: the MR already exists, so failing here would redden a perfectly good job.
	// Recorded either way, or the report shows a clean success for an MR that will never merge.
	if r.config.ActiveRunType().AutoMerge {
		err := r.platform.EnableAutoMerge(ctx, mr)
		r.rec.SetAutoMerge(err)
		if err != nil {
			r.logger.Warn("failed to enable auto merge", zap.String("url", mr.URL), zap.Error(err))
		} else {
			r.logger.Info("auto merge enabled", zap.String("url", mr.URL))
		}
	}

	return nil
}
