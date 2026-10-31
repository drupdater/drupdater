package cmd

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/drupdater/drupdater/internal/config"
	"github.com/drupdater/drupdater/internal/repo"
	"go.uber.org/zap"
)

// resolveCheckoutSettings fills the URL and target branch from the checkout, overwriting --branch.
func resolveCheckoutSettings(ctx context.Context, git *repo.Git, cfg *config.Config) error {
	if cfg.RepositoryURL == "" {
		remoteURL, err := git.GetRemoteURL(ctx, cfg.WorkingDir)
		if err != nil {
			return fmt.Errorf("failed to determine repository URL from checkout (pass --repository-url or fix the checkout): %w", err)
		}
		cfg.RepositoryURL = remoteURL
	}

	branch, err := resolveCheckoutBranch(ctx, git, cfg.WorkingDir)
	if err != nil {
		return err
	}
	cfg.Branch = branch
	return nil
}

// resolveCheckoutBranch returns the checkout's branch, or the CI variable when in detached HEAD.
func resolveCheckoutBranch(ctx context.Context, git *repo.Git, workingDir string) (string, error) {
	branch, err := git.GetCurrentBranch(ctx, workingDir)
	if err != nil {
		return "", fmt.Errorf("failed to determine branch from checkout: %w", err)
	}
	if branch == "" {
		branch = cmp.Or(os.Getenv("GITHUB_REF_NAME"), os.Getenv("CI_COMMIT_REF_NAME"))
	}
	if branch == "" {
		return "", errors.New("could not determine the target branch: the checkout is in detached HEAD and no CI branch variable (GITHUB_REF_NAME, CI_COMMIT_REF_NAME) is set")
	}
	return branch, nil
}

// ensureGitSafeDirectory trusts dir globally, skipping it when already listed so repeated runs
// don't append duplicates to the user's gitconfig.
func ensureGitSafeDirectory(ctx context.Context, logger *zap.Logger, dir string) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		logger.Warn("failed to resolve checkout path for safe.directory", zap.Error(err))
		return
	}

	if out, err := exec.CommandContext(ctx, "git", "config", "--global", "--get-all", "safe.directory").Output(); err == nil {
		for entry := range strings.SplitSeq(string(out), "\n") {
			if entry == abs || entry == "*" {
				return
			}
		}
	}

	if out, err := exec.CommandContext(ctx, "git", "config", "--global", "--add", "safe.directory", abs).CombinedOutput(); err != nil {
		logger.Warn("failed to mark checkout as a safe git directory", zap.String("output", string(out)), zap.Error(err))
	}
}
