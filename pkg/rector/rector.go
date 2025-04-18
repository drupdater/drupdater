package rector

import (
	"context"
	"os/exec"
	"strings"

	"github.com/spf13/afero"
	"go.uber.org/zap"
)

var execCommand = exec.CommandContext

type RectorService interface {
	Run(ctx context.Context, dir string, customCodeDirectories []string) (string, error)
}

type DefaultRectorService struct {
	fs     afero.Fs
	logger *zap.Logger
}

func NewDefaultRectorService(logger *zap.Logger) *DefaultRectorService {
	return &DefaultRectorService{
		fs:     afero.NewOsFs(),
		logger: logger,
	}
}

func (s *DefaultRectorService) execComposer(ctx context.Context, dir string, args ...string) (string, error) {
	command := execCommand(ctx, "composer", args...)
	command.Dir = dir

	out, err := command.CombinedOutput()
	output := strings.TrimSuffix(string(out), "\n")

	s.logger.Debug("executing composer", zap.String("dir", dir), zap.Strings("args", args), zap.String("output", output))

	return output, err
}

func (s *DefaultRectorService) Run(ctx context.Context, dir string, customCodeDirectories []string) (string, error) {
	s.logger.Debug("remove deprecations")

	if len(customCodeDirectories) == 0 {
		s.logger.Debug("no custom code directories found")
		return `{
    "totals": {
        "changed_files": 0,
        "errors": 0
    },
    "file_diffs": [],
    "changed_files": []
}`, nil

	}

	args := []string{"exec", "--", "rector", "process", "--config=/opt/drupdater/rector.php", "--no-progress-bar", "--no-diffs", "--debug", "--output-format=json"}
	args = append(args, customCodeDirectories...)

	return s.execComposer(ctx, dir, args...)
}
