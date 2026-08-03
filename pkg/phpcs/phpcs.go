package phpcs

import (
	"context"
	"encoding/json"
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

// command runs phpcs through `composer exec`, inheriting its process timeout.
func (s *CLI) command(dir string) composer.Command {
	return composer.Command{New: execCommand, Logger: s.logger, Dir: dir}
}

type ReturnOutput struct {
	Files  map[string]ReturnOutputFile `json:"files"`
	Totals ReturnOutputTotals          `json:"totals"`
}

type ReturnOutputFile struct {
	Errors   int                       `json:"errors"`
	Warnings int                       `json:"warnings"`
	Messages []ReturnOutputFileMessage `json:"messages"`
}

type ReturnOutputFileMessage struct {
	Message  string `json:"message"`
	Source   string `json:"source"`
	Severity int    `json:"severity"`
	Fixable  bool   `json:"fixable"`
	Type     string `json:"type"`
	Line     int    `json:"line"`
	Column   int    `json:"column"`
}
type ReturnOutputTotals struct {
	Errors   int `json:"errors"`
	Warnings int `json:"warnings"`
	Fixable  int `json:"fixable"`
}

// Run reports the violations. Split: a PHP notice on stderr would corrupt the JSON report.
func (s *CLI) Run(ctx context.Context, dir string) (ReturnOutput, error) {
	out, _, err := s.command(dir).Split(ctx, "exec", "--", "phpcs", "--report=json", "-q", "--runtime-set", "ignore_errors_on_exit", "1", "--runtime-set", "ignore_warnings_on_exit", "1")
	if err != nil {
		return ReturnOutput{}, err
	}
	var codingStyleUpdateResult ReturnOutput
	if err := json.Unmarshal([]byte(out), &codingStyleUpdateResult); err != nil {
		return ReturnOutput{}, err
	}
	return codingStyleUpdateResult, nil

}

func (s *CLI) RunCBF(ctx context.Context, dir string) error {
	_, err := s.command(dir).Combined(ctx, "exec", "--", "phpcbf")
	return err
}
