package rector

import (
	"context"
	"os/exec"
	"strings"

	"go.uber.org/zap"
)

var execCommand = exec.CommandContext

type Runner interface {
	Run(ctx context.Context, dir string, customCodeDirectories []string) (string, error)
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

	s.logger.Sugar().Debugf("%s\n%s", command.String(), output)

	return output, err
}

func (s *CLI) Run(ctx context.Context, dir string, customCodeDirectories []string) (string, error) {
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
