package composer

import (
	"context"

	"github.com/drupdater/drupdater/internal/agent"
)

// CheckAgent confirms the drupdater agent is installed in composer and speaks this binary's protocol.
func (s *CLI) CheckAgent(ctx context.Context, dir string) error {
	_, err := agent.Call[struct{}](ctx, s.command(dir), "drupdater:handshake", struct{}{})
	return err
}
