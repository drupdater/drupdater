package addon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/drupdater/drupdater/internal/agent"
	"github.com/drupdater/drupdater/internal/workflow"
	"go.uber.org/zap"
)

// hookRunner runs one addon's hook in the Composer agent, as its tool interface's RunHook does.
type hookRunner func(ctx context.Context, dir string, addon string, event any) (agent.HookReply, error)

type agentEvent interface {
	Context() context.Context
	Path() string
}

// agentHook is a hook whose work runs in the agent. Its finding is decoded into F refusing unknown
// fields, so a field the agent adds cannot reach the published report unreviewed.
func agentHook[E agentEvent, F any](logger *zap.Logger, run hookRunner, addon string, payload func(E) any) func(E) (workflow.Result[F], error) {
	return func(e E) (workflow.Result[F], error) {
		reply, err := run(e.Context(), e.Path(), addon, payload(e))
		if err != nil {
			return workflow.Result[F]{}, fmt.Errorf("%s: %w", addon, err)
		}

		var finding F
		if len(reply.Finding) > 0 && string(reply.Finding) != "null" {
			decoder := json.NewDecoder(bytes.NewReader(reply.Finding))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&finding); err != nil {
				return workflow.Result[F]{}, fmt.Errorf("%s: failed to decode the agent's finding: %w", addon, err)
			}
		}

		for _, line := range reply.Log {
			logger.Info(line, zap.String("addon", addon))
		}

		var commits []workflow.Commit
		for _, c := range reply.Commits {
			commits = append(commits, workflow.Commit{Message: c.Message, Paths: c.Paths})
		}
		return workflow.Result[F]{Commits: commits, Finding: finding}, nil
	}
}

type postComposerUpdatePayload struct {
	Name     string          `json:"name"`
	BaseLock json.RawMessage `json:"base_lock"`
}

func postComposerUpdateEvent(e *workflow.PostComposerUpdateEvent) any {
	return postComposerUpdatePayload{Name: workflow.EventPostComposerUpdate, BaseLock: e.BaseLock()}
}
