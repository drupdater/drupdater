package workflow

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/drupdater/drupdater/internal/buildinfo"
	"github.com/drupdater/drupdater/internal/config"
	"github.com/drupdater/drupdater/internal/repo"
	"github.com/drupdater/drupdater/internal/report"

	"go.uber.org/zap"
)

// reportTimeout bounds building the addons' report sections once the run is over.
const reportTimeout = time.Minute

// Runner updates a project. It holds only what all its runs share, so it can run again.
type Runner struct {
	services

	// reportSink receives the run report on every exit path. nil when --report was not given.
	reportSink func(report.Report)
}

// services are a Runner's dependencies, shared read-only by all its runs.
type services struct {
	logger   *zap.Logger
	config   config.Config
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
	pipeline *Pipeline
	rec      *report.Recorder

	repository GitRepository
	worktree   Worktree
	path       string
	// author signs the run's own commits, as committer signs the addons'.
	author    repo.Identity
	committer Committer
}

func newRun(s services, checkout repo.Checkout, pipeline *Pipeline, rec *report.Recorder) *run {
	return &run{
		services:   s,
		pipeline:   pipeline,
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
	cfg config.Config,
	drush Drush,
	platform Platform,
	gitService GitService,
	installer Installer,
	composerService Composer,
	opts ...Option,
) *Runner {
	r := &Runner{services: services{
		logger:    logger,
		config:    cfg,
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
	rec := report.NewRecorder(buildinfo.Version, mode, r.config.DryRun, r.config.RepositoryURL, r.config.Branch, r.config.Sites)
	pipeline := NewPipeline(addons, r.composer, r.config.Security)

	// Registered first, so it runs last and covers every exit path. An AbortError means there
	// was nothing to do, not that the run failed.
	defer func() {
		if r.reportSink == nil {
			return
		}
		if _, ok := errors.AsType[AbortError](err); ok {
			rec.SetNoChanges()
		}
		// Bounded on its own: the run's own deadline may be what ended it.
		reportCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), reportTimeout)
		defer cancel()
		if reportErr := pipeline.Report(reportCtx, rec); reportErr != nil {
			r.logger.Warn("the report lacks the addons' sections", zap.Error(reportErr))
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

	branch, err := newRun(r.services, checkout, pipeline, rec).execute(ctx)
	if err != nil {
		r.removeUpdateBranch(context.WithoutCancel(ctx), checkout.Repository, branch)
	}
	return err
}

// execute runs the phases, returning the update branch it created ("" if none) with any error.
func (r *run) execute(ctx context.Context) (branch string, err error) {
	// Fail fast on the prerequisites "drupdater check" shares. Extension requirements are
	// deliberately not among them — see CheckPlatformReqs.
	if err := r.rec.Run("preflight", func() error { return r.preflight(ctx) }); err != nil {
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
		if err := r.createUpdateBranch(ctx, updateBranchName); err != nil {
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
		mrTitle, mrDescription, renderErr = r.renderMergeRequest(ctx)
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

func (r *run) preflight(ctx context.Context) error {
	if result := CheckGitHistoryComplete(ctx, r.git, r.path); !result.OK {
		return fmt.Errorf("%s: %s", result.Name, result.Detail)
	}
	// The platform check is an agent command, so a missing agent has to be reported first.
	if result := CheckComposerAgent(ctx, r.composer, r.path); !result.OK {
		return fmt.Errorf("%s: %s", result.Name, result.Detail)
	}
	if result := CheckPlatformRequirements(ctx, r.composer, r.path); !result.OK {
		return fmt.Errorf("PHP platform requirements not satisfied:\n%s", result.Detail)
	}
	return nil
}

// acquireWorkingCopy clones in clone mode. In checkout mode it adds a linked worktree of the
// checkout, so the run never changes the user's own working tree, branch or git config.
func (s services) acquireWorkingCopy(ctx context.Context, username, email string) (repo.Checkout, error) {
	requested := repo.Identity{Name: username, Email: email}
	if s.config.Clone {
		s.logger.Info("cloning repository", zap.String("url", s.config.RepositoryURL), zap.String("branch", s.config.Branch))
		return s.git.CloneRepository(ctx, s.config.RepositoryURL, s.config.Branch, s.config.Token, requested)
	}
	return s.git.AddWorktree(ctx, s.config.WorkingDir, requested)
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
