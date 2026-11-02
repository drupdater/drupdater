package composer

import (
	"context"

	"github.com/drupdater/drupdater/internal/agent"
)

type handshakeResult struct {
	ComposerVersion string `json:"composer_version"`
	PHPVersion      string `json:"php_version"`
}

// CheckAgent confirms the drupdater agent is installed in composer and speaks this binary's protocol.
func (s *CLI) CheckAgent(ctx context.Context, dir string) error {
	_, err := agent.Call[handshakeResult](ctx, s.command(dir), "drupdater:handshake", struct{}{})
	return err
}
