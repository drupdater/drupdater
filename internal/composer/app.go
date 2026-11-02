package composer

import (
	"context"

	"github.com/drupdater/drupdater/internal/agent"
	"github.com/drupdater/drupdater/internal/php"
)

// AppPath is where the image installs drupdater's PHP app, app/ in this repository.
const AppPath = "/opt/drupdater/app/bin/drupdater"

// app runs the app in dir. It runs composer for the hooks, so it gets composer's environment.
func (s *CLI) app(dir string) php.Command {
	c := s.command(dir)
	c.Program = AppPath
	return c
}

// Fire runs every addon's hook of one event in the project in dir, through the app.
func (s *CLI) Fire(ctx context.Context, dir string, request agent.FireRequest) (agent.FireReply, error) {
	return agent.Call[agent.FireReply](ctx, s.app(dir), "internal:fire", request)
}

// Commit makes commits of exactly the paths each names in the working copy in dir, through the app.
func (s *CLI) Commit(ctx context.Context, dir string, request agent.CommitRequest) (agent.CommitReply, error) {
	return agent.Call[agent.CommitReply](ctx, s.app(dir), "internal:commit", request)
}
