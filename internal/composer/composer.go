package composer

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/drupdater/drupdater/internal/agent"
	"github.com/drupdater/drupdater/internal/php"

	"go.uber.org/zap"
)

type CLI struct {
	logger     *zap.Logger
	newCommand php.CommandFactory
	// extraEnv is added to every invocation; see WithoutBlocking.
	extraEnv []string
}

func NewCLI(logger *zap.Logger, opts ...Option) *CLI {
	s := &CLI{
		logger:     logger,
		newCommand: exec.CommandContext,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Option configures a CLI.
type Option func(*CLI)

// WithoutBlocking lets composer resolve to versions its policy blocks, such as ones with security
// advisories. A security run needs it: an advisory with no fixed version in range would otherwise
// make every update unresolvable, holding back the fixes that do exist.
func WithoutBlocking() Option {
	return func(s *CLI) {
		s.extraEnv = append(s.extraEnv, "COMPOSER_NO_BLOCKING=1")
	}
}

func (s *CLI) command(dir string) php.Command {
	return php.Command{New: s.newCommand, Logger: s.logger, Dir: dir, ExtraEnv: s.extraEnv}
}

// Versions are the tool versions a run's outcome depends on.
type Versions struct {
	Composer string
	PHP      string
}

// Version reports the Composer and PHP the agent runs under. Not tied to a project.
func (s *CLI) Version(ctx context.Context) (Versions, error) {
	reply, err := withoutProject(func(dir string) (handshakeResult, error) {
		return agent.Call[handshakeResult](ctx, s.command(dir), "drupdater:handshake", struct{}{})
	})
	if err != nil {
		return Versions{}, fmt.Errorf("failed to determine the composer version: %w", err)
	}
	return Versions{Composer: reply.ComposerVersion, PHP: reply.PHPVersion}, nil
}

// Install brings vendor/ to composer.lock.
func (s *CLI) Install(ctx context.Context, dir string) error {
	if _, err := agent.Call[struct{}](ctx, s.command(dir), "drupdater:install", struct{}{}); err != nil {
		return fmt.Errorf("failed to install dependencies: %w", err)
	}
	return nil
}
