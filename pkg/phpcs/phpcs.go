package phpcs

import (
	"context"
	"os/exec"
	"strings"

	"go.uber.org/zap"
)

var execCommand = exec.CommandContext

type Runner interface {
	Run(ctx context.Context, dir string) (string, error)
	RunCBF(ctx context.Context, dir string) error
}

type CLI struct {
	logger *zap.Logger
}

func NewCLI(logger *zap.Logger) *CLI {
	return &CLI{
		logger: logger,
	}
}

func (s *CLI) execComposer(ctx context.Context, dir string, args ...string) (string, error) {
	command := execCommand(ctx, "composer", args...)
	command.Dir = dir

	out, err := command.CombinedOutput()
	output := strings.TrimSuffix(string(out), "\n")

	s.logger.Debug("executing composer", zap.String("dir", dir), zap.Strings("args", args), zap.String("output", output))

	return output, err
}

func (s *CLI) Run(ctx context.Context, dir string) (string, error) {
	s.logger.Debug("running phpcs")
	return s.execComposer(ctx, dir, "exec", "--", "phpcs", "--report=json", "-q", "--runtime-set", "ignore_errors_on_exit", "1", "--runtime-set", "ignore_warnings_on_exit", "1")
}

func (s *CLI) RunCBF(ctx context.Context, dir string) error {
	s.logger.Debug("running phpcbf")
	_, err := s.execComposer(ctx, dir, "exec", "--", "phpcbf")
	return err
}
