package addon

import (
	"context"

	"github.com/drupdater/drupdater/internal/agent"
	"github.com/drupdater/drupdater/internal/workflow"
	"go.uber.org/zap"
)

// DiffAgent runs composer_diff's hook in the Composer agent, which diffs the lock there.
type DiffAgent interface {
	RunHook(ctx context.Context, dir string, addon string, event any) (agent.HookReply, error)
}

type ComposerDiff struct {
	logger *zap.Logger
	agent  DiffAgent
}

func NewComposerDiff(logger *zap.Logger, agent DiffAgent) *ComposerDiff {
	return &ComposerDiff{
		logger: logger,
		agent:  agent,
	}
}

func (cd *ComposerDiff) Hooks() workflow.Hooks {
	return workflow.Hooks{
		PostComposerUpdate: workflow.Produce(workflow.Inspect, agentHook[*workflow.PostComposerUpdateEvent, []DependencyChange](
			cd.logger, cd.runHook, cd.ReportKey(), postComposerUpdateEvent)),
	}
}

// runHook defers to agent at call time: Hooks is also read off an addon built without one.
func (cd *ComposerDiff) runHook(ctx context.Context, dir string, addon string, event any) (agent.HookReply, error) {
	return cd.agent.RunHook(ctx, dir, addon, event)
}
