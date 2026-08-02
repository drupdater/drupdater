package composer

import (
	"bytes"
	"context"
	"os/exec"
	"strings"

	"go.uber.org/zap"
)

// CommandFactory builds the *exec.Cmd to run. Every package that shells out to composer keeps
// its own package-level variable of this type as its test seam and passes it in per call, so
// swapping the variable still diverts the subprocess.
type CommandFactory func(ctx context.Context, name string, args ...string) *exec.Cmd

// Command is one composer invocation: run in Dir, under the environment Env guarantees.
//
// Shared by every package that drives composer — drush, phpcs and rector all run through
// `composer exec` — so the process timeout and the debug logging are stated once.
type Command struct {
	New    CommandFactory
	Logger *zap.Logger
	Dir    string
	// ExtraEnv is appended after Env, so it wins over anything inherited — drush's SITE_NAME.
	ExtraEnv []string
}

func (c Command) build(ctx context.Context, args ...string) *exec.Cmd {
	command := c.New(ctx, "composer", args...)
	command.Dir = c.Dir
	// Environ() falls back to os.Environ(), so this layers on the deployment's environment
	// instead of replacing it.
	command.Env = append(Env(command.Environ()), c.ExtraEnv...)
	return command
}

// Combined runs the command with stdout and stderr merged, as CombinedOutput does, and returns
// the output with its trailing newline stripped.
func (c Command) Combined(ctx context.Context, args ...string) (string, error) {
	command := c.build(ctx, args...)

	// One buffer for both streams: what CombinedOutput does internally, kept here because
	// CombinedOutput refuses a command whose Stdout is already set.
	var merged bytes.Buffer
	command.Stdout = &merged
	command.Stderr = &merged
	err := command.Run()

	output := strings.TrimSuffix(merged.String(), "\n")
	c.Logger.Debug(command.String() + "\n" + output)

	return output, err
}

// Split keeps the streams apart. Commands whose stdout is parsed as JSON must use this: a
// composer or PHP notice on stderr would otherwise corrupt the payload. stderr still reaches
// the log, and is returned so a caller can quote it in an error.
func (c Command) Split(ctx context.Context, args ...string) (stdout string, stderr string, err error) {
	command := c.build(ctx, args...)

	var so, se bytes.Buffer
	command.Stdout = &so
	command.Stderr = &se
	err = command.Run()

	stdout = strings.TrimSuffix(so.String(), "\n")
	stderr = strings.TrimSuffix(se.String(), "\n")
	c.Logger.Debug(command.String() + "\nstdout: " + stdout + "\nstderr: " + stderr)

	return stdout, stderr, err
}
