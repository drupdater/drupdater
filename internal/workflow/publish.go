package workflow

import (
	"context"
	"fmt"

	"github.com/drupdater/drupdater/internal/mergerequest"
	"go.uber.org/zap"
)

// renderMergeRequest renders from the report as it stands, so the merge request says what the
// report says. Every addon has finished by now, so the final report's sections are these.
func (r *run) renderMergeRequest(ctx context.Context) (string, string, error) {
	if err := r.pipeline.Report(ctx, r.rec); err != nil {
		return "", "", err
	}
	snapshot := r.rec.Snapshot()

	description, err := mergerequest.Description(snapshot)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate description: %w", err)
	}

	return mergerequest.Title(snapshot), description, nil
}

func (r *run) publish(ctx context.Context, updateBranchName, title, description string) error {
	err := r.repository.Push(ctx, updateBranchName, r.config.Token)
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
