package workflow

import (
	"errors"
	"fmt"

	"github.com/go-git/go-git/v5/plumbing"
	"go.uber.org/zap"
)

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
