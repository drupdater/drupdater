package phpcs

import (
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"

	"go.uber.org/zap"

	"github.com/drupdater/drupdater/pkg/composer"
)

var execCommand = exec.CommandContext

type CLI struct {
	logger *zap.Logger
	// binDir holds drupdater's own phpcs and phpcbf; empty runs the project's through composer.
	binDir string
}

// NewCLI runs the project's own phpcs and phpcbf.
func NewCLI(logger *zap.Logger) *CLI {
	return &CLI{
		logger: logger,
	}
}

// NewBundledCLI runs the phpcs and phpcbf in binDir, which need nothing installed in the project.
func NewBundledCLI(logger *zap.Logger, binDir string) *CLI {
	return &CLI{
		logger: logger,
		binDir: binDir,
	}
}

// invoke returns the command running tool in dir, and the arguments that must precede tool's own.
func (s *CLI) invoke(dir string, tool string) (composer.Command, []string) {
	command := composer.Command{New: execCommand, Logger: s.logger, Dir: dir}
	if s.binDir == "" {
		return command, []string{"exec", "--", tool}
	}
	command.Program = filepath.Join(s.binDir, tool)
	return command, nil
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
	command, args := s.invoke(dir, "phpcs")
	args = append(args, "--report=json", "-q", "--runtime-set", "ignore_errors_on_exit", "1", "--runtime-set", "ignore_warnings_on_exit", "1")
	out, _, err := command.Split(ctx, args...)
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
	command, args := s.invoke(dir, "phpcbf")
	_, err := command.Combined(ctx, args...)
	return err
}
