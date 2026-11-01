package composer

import (
	"context"
	"fmt"
	"os"

	"github.com/drupdater/drupdater/internal/agent"
)

// Addons lists the agent's addon catalogue. It needs no project, so it asks from an empty
// directory, where no project's own plugins load.
func (s *CLI) Addons(ctx context.Context) ([]agent.Addon, error) {
	result, err := withoutProject(func(dir string) (struct {
		Addons []agent.Addon `json:"addons"`
	}, error) {
		return agent.Call[struct {
			Addons []agent.Addon `json:"addons"`
		}](ctx, s.command(dir), "drupdater:addons", struct{}{})
	})
	if err != nil {
		return nil, fmt.Errorf("failed to read the addon catalogue: %w", err)
	}
	return result.Addons, nil
}

// RunHook runs one addon's hook for one event in the project in dir.
func (s *CLI) RunHook(ctx context.Context, dir string, request agent.HookRequest) (agent.HookReply, error) {
	return agent.Call[agent.HookReply](ctx, s.command(dir), "drupdater:hook", request)
}

// Report builds the addons' report sections from what their hooks found. A section is a function
// of its findings alone, so it needs no project: it can be asked for after the working copy is gone.
func (s *CLI) Report(ctx context.Context, request agent.ReportRequest) (agent.ReportReply, error) {
	reply, err := withoutProject(func(dir string) (agent.ReportReply, error) {
		return agent.Call[agent.ReportReply](ctx, s.command(dir), "drupdater:report", request)
	})
	if err != nil {
		return agent.ReportReply{}, fmt.Errorf("failed to build the addons' report sections: %w", err)
	}
	return reply, nil
}

func withoutProject[T any](call func(dir string) (T, error)) (T, error) {
	var zero T
	dir, err := os.MkdirTemp("", "drupdater-agent-")
	if err != nil {
		return zero, err
	}
	defer os.RemoveAll(dir)
	return call(dir)
}
