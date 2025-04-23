package rector

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"go.uber.org/zap"
)

var execCommand = exec.CommandContext

type Runner interface {
	Run(ctx context.Context, dir string, customCodeDirectories []string) (ReturnOutput, error)
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

type ReturnOutput struct {
	Totals       ReturnOutputTotals     `json:"totals"`
	FileDiffs    []ReturnOutputFillDiff `json:"file_diffs"`
	ChangedFiles []string               `json:"changed_files"`
}

type ReturnOutputTotals struct {
	ChangedFiles int `json:"changed_files"`
	Errors       int `json:"errors"`
}

type ReturnOutputFillDiff struct {
	File           string   `json:"file"`
	Diff           string   `json:"diff"`
	AppliedRectors []string `json:"applied_rectors"`
}

func (s *CLI) Run(ctx context.Context, dir string, customCodeDirectories []string) (ReturnOutput, error) {
	if len(customCodeDirectories) == 0 {
		s.logger.Debug("no custom code directories found")
		return ReturnOutput{
			Totals: ReturnOutputTotals{
				ChangedFiles: 0,
				Errors:       0,
			},
			FileDiffs:    []ReturnOutputFillDiff{},
			ChangedFiles: []string{},
		}, nil
	}

	args := []string{"exec", "--", "rector", "process", "--config=/opt/drupdater/rector.php", "--no-progress-bar", "--no-diffs", "--debug", "--output-format=json"}
	args = append(args, customCodeDirectories...)

	out, err := s.execComposer(ctx, dir, args...)

	if err != nil {
		return ReturnOutput{}, fmt.Errorf("failed to run composer command: %w", err)
	}

	var deprecationRemovalResult ReturnOutput
	if err := json.Unmarshal([]byte(out), &deprecationRemovalResult); err != nil {
		return ReturnOutput{}, fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	return deprecationRemovalResult, nil
}
