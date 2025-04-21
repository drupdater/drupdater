package phpcs

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"

	"go.uber.org/zap"
)

var execCommand = exec.CommandContext

type Runner interface {
	Run(ctx context.Context, dir string) (ReturnOutput, error)
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

func (s *CLI) Run(ctx context.Context, dir string) (ReturnOutput, error) {
	s.logger.Debug("running phpcs")
	out, err := s.execComposer(ctx, dir, "exec", "--", "phpcs", "--report=json", "-q", "--runtime-set", "ignore_errors_on_exit", "1", "--runtime-set", "ignore_warnings_on_exit", "1")
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
	s.logger.Debug("running phpcbf")
	_, err := s.execComposer(ctx, dir, "exec", "--", "phpcbf")
	return err
}
