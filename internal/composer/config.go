package composer

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/drupdater/drupdater/internal/agent"
)

// InvalidConfigError is a .drupdater.yaml the agent refused; the reason says what to fix.
type InvalidConfigError struct {
	Reason string
}

func (e *InvalidConfigError) Error() string { return e.Reason }

// Config reads and checks the .drupdater.yaml at path, which need not exist. It needs no project,
// so it asks from an empty directory, where no project's own plugins load.
func (s *CLI) Config(ctx context.Context, path string) (agent.ConfigReply, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return agent.ConfigReply{}, err
	}
	reply, err := withoutProject(func(dir string) (agent.ConfigReply, error) {
		return agent.Call[agent.ConfigReply](ctx, s.command(dir), "drupdater:config", agent.ConfigRequest{Path: absolute})
	})
	if agentErr, ok := errors.AsType[*agent.Error](err); ok && agentErr.Kind == agent.Failed {
		return agent.ConfigReply{}, &InvalidConfigError{Reason: agentErr.Reason}
	}
	if err != nil {
		return agent.ConfigReply{}, fmt.Errorf("failed to read the configuration: %w", err)
	}
	return reply, nil
}
