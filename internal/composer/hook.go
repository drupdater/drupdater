package composer

import (
	"context"

	"github.com/drupdater/drupdater/internal/agent"
)

type hookRequest struct {
	Addon string `json:"addon"`
	Event any    `json:"event"`
}

// RunHook runs addon's hook for event in the agent's drupdater:addon, event being the payload
// the agent reads for it, its name included.
func (s *CLI) RunHook(ctx context.Context, dir string, addon string, event any) (agent.HookReply, error) {
	return agent.Call[agent.HookReply](ctx, s.command(dir), "drupdater:addon", hookRequest{Addon: addon, Event: event})
}
