package rector

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"

	"go.uber.org/zap"

	"github.com/drupdater/drupdater/pkg/composer"
)

var execCommand = exec.CommandContext

type CLI struct {
	logger *zap.Logger
}

func NewCLI(logger *zap.Logger) *CLI {
	return &CLI{
		logger: logger,
	}
}

// command runs rector through `composer exec`, inheriting its process timeout.
func (s *CLI) command(dir string) composer.Command {
	return composer.Command{New: execCommand, Logger: s.logger, Dir: dir}
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
			// Equivalent mutant, left un-suppressed: the annotation keys off the enclosing
			// literal and would silence the real FileDiffs and ChangedFiles mutants too.
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

	// Split: rector's --debug output and PHP notices would corrupt the JSON report.
	out, _, err := s.command(dir).Split(ctx, args...)

	if err != nil {
		return ReturnOutput{}, fmt.Errorf("failed to run composer command: %w", err)
	}

	var deprecationRemovalResult ReturnOutput
	if err := json.Unmarshal([]byte(out), &deprecationRemovalResult); err != nil {
		return ReturnOutput{}, fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	return deprecationRemovalResult, nil
}
