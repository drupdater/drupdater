package workflow

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"go.uber.org/zap"

	"github.com/drupdater/drupdater/internal/composer"
	"github.com/drupdater/drupdater/internal/report"
)

// stageScaffoldChanges stages the web-root files drupal-scaffold rewrites on a core update --
// .htaccess, robots.txt, index.php -- which the composer.* glob does not cover.
//
// Tracked paths only: vendor/ and web/core legitimately sit untracked in the checkout.
func (r *run) stageScaffoldChanges(ctx context.Context) error {
	changed, err := r.worktree.Unstaged(ctx)
	if err != nil {
		return fmt.Errorf("failed to read worktree status: %w", err)
	}
	if len(changed) == 0 {
		return nil
	}

	// Looked up only when there is something to stage, to save a composer subprocess.
	project, err := r.composer.Project(ctx, r.path)
	if err != nil {
		return fmt.Errorf("failed to determine web root: %w", err)
	}
	webroot, err := project.RequireWebRoot()
	if err != nil {
		return fmt.Errorf("failed to determine web root: %w", err)
	}
	// Web root only: everything outside it belongs to a later phase, and sweeping it in here
	// would commit it mid-update, still carrying the baseline install's state.
	staged := slices.DeleteFunc(slices.Sorted(slices.Values(changed)), func(file string) bool {
		return !strings.HasPrefix(file, webroot+"/")
	})
	if len(staged) == 0 {
		return nil
	}
	if err := r.worktree.Stage(ctx, staged...); err != nil {
		return fmt.Errorf("failed to stage %s: %w", strings.Join(staged, ", "), err)
	}
	r.logger.Info("staged scaffold changes", zap.Strings("files", staged))
	return nil
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
	project, err := r.composer.Project(ctx, r.path)
	if err != nil {
		return "", err
	}
	composerLockHash, err := project.RequireLockHash()
	if err != nil {
		return "", err
	}
	updateBranchName := fmt.Sprintf("update-%s", composerLockHash)

	if err := r.ensureUpdateBranchAvailable(ctx, updateBranchName); err != nil {
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

	if err := r.worktree.StageGlob(ctx, "composer.*"); err != nil {
		return fmt.Errorf("failed to add composer.* files: %w", err)
	}
	if err := r.stageScaffoldChanges(ctx); err != nil {
		return err
	}
	if err := r.worktree.Commit(ctx, "Update composer.json and composer.lock", r.author); err != nil {
		return fmt.Errorf("failed to commit composer.json and composer.lock: %w", err)
	}

	postCodeUpdateEvent := NewPostCodeUpdateEvent(ctx, r.path, r.committer)
	if err := Fire(r.hooks, postCodeUpdateEvent); err != nil {
		return err
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
