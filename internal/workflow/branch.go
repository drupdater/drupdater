package workflow

import (
	"context"
	"fmt"

	"go.uber.org/zap"
)

// removeUpdateBranch deletes the update branch a failed run created, or the next run would find
// it and stop as if there were nothing to do. "" means the run created none.
func (s services) removeUpdateBranch(ctx context.Context, repository GitRepository, branch string) {
	if branch == "" {
		return
	}
	if err := repository.RemoveBranch(ctx, branch); err != nil {
		s.logger.Warn("failed to remove the failed run's update branch", zap.String("branch", branch), zap.Error(err))
	}
}

// createUpdateBranch points updateBranchName at HEAD. Only once the last commit is made: the run
// commits on a detached HEAD, which a branch created earlier would not follow.
func (r *run) createUpdateBranch(ctx context.Context, updateBranchName string) error {
	head, err := r.repository.Head(ctx)
	if err != nil {
		return fmt.Errorf("failed to read HEAD: %w", err)
	}
	if err := r.repository.SetBranch(ctx, updateBranchName, head); err != nil {
		return fmt.Errorf("failed to create branch %s: %w", updateBranchName, err)
	}
	return nil
}

// ensureUpdateBranchAvailable returns an AbortError if updateBranchName is already taken, locally
// or on the remote, and a plain error if either check itself fails. A prior failed run leaves its
// local branch behind, which would otherwise make the branch creation fail on git's own message.
func (r *run) ensureUpdateBranchAvailable(ctx context.Context, updateBranchName string) error {
	local, err := r.repository.BranchExists(ctx, updateBranchName)
	if err != nil {
		return fmt.Errorf("failed to check for a local %s branch: %w", updateBranchName, err)
	}
	if local {
		return AbortError{Msg: fmt.Sprintf("branch %s already exists locally, skipping", updateBranchName)}
	}

	// Skipped for a dry run, which pushes nothing and may have no token to list the remote with.
	if r.config.DryRun {
		return nil
	}

	exists, err := r.repository.RemoteBranchExists(ctx, updateBranchName, r.config.Token)
	if err != nil {
		return fmt.Errorf("failed to check if branch exists: %w", err)
	}
	if exists {
		return AbortError{Msg: fmt.Sprintf("branch %s already exists, skipping", updateBranchName)}
	}
	return nil
}
