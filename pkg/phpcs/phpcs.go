package phpcs

import (
	"context"
	"os/exec"
	"strings"

	"github.com/spf13/afero"
	"go.uber.org/zap"
)

var execCommand = exec.CommandContext

type PhpCsService interface {
	Run(ctx context.Context, dir string) (string, error)
	RunCBF(ctx context.Context, dir string) error
}

type DefaultPhpCsService struct {
	fs     afero.Fs
	logger *zap.Logger
}

func NewDefaultPhpCsService(logger *zap.Logger) *DefaultPhpCsService {
	return &DefaultPhpCsService{
		fs:     afero.NewOsFs(),
		logger: logger,
	}
}

func (s *DefaultPhpCsService) execComposer(ctx context.Context, dir string, args ...string) (string, error) {
	command := execCommand(ctx, "composer", args...)
	command.Dir = dir

	out, err := command.CombinedOutput()
	output := strings.TrimSuffix(string(out), "\n")

	s.logger.Debug("executing composer", zap.String("dir", dir), zap.Strings("args", args), zap.String("output", output))

	return output, err
}

func (s *DefaultPhpCsService) Run(ctx context.Context, dir string) (string, error) {
	s.logger.Debug("running phpcs")
	return s.execComposer(ctx, dir, "exec", "--", "phpcs", "--report=json", "-q", "--runtime-set", "ignore_errors_on_exit", "1", "--runtime-set", "ignore_warnings_on_exit", "1")
}

func (s *DefaultPhpCsService) RunCBF(ctx context.Context, dir string) error {
	s.logger.Debug("running phpcbf")
	_, err := s.execComposer(ctx, dir, "exec", "--", "phpcbf")
	return err
}
