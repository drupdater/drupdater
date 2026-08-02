package rector

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

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

// execComposerJSON returns stdout only: rector's --debug output and PHP notices go to stderr
// and would corrupt the JSON report.
func (s *CLI) execComposerJSON(ctx context.Context, dir string, args ...string) (string, error) {
	command := execCommand(ctx, "composer", args...)
	command.Dir = dir
	// rector runs through `composer exec` and inherits composer's process timeout.
	command.Env = composer.Env(command.Environ())

	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()

	output := strings.TrimSuffix(stdout.String(), "\n")
	s.logger.Debug(command.String() + "\nstdout: " + output + "\nstderr: " + strings.TrimSuffix(stderr.String(), "\n"))

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
			// Known equivalent mutant: dropping this yields the same ReturnOutputTotals{}. Left
			// un-suppressed because the annotation keys off the enclosing literal and would
			// also silence the real FileDiffs and ChangedFiles mutants. The zeros are spelled
			// out because this is the documented "nothing to do" result.
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

	out, err := s.execComposerJSON(ctx, dir, args...)

	if err != nil {
		return ReturnOutput{}, fmt.Errorf("failed to run composer command: %w", err)
	}

	var deprecationRemovalResult ReturnOutput
	if err := json.Unmarshal([]byte(out), &deprecationRemovalResult); err != nil {
		return ReturnOutput{}, fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	return deprecationRemovalResult, nil
}
