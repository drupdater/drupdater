package composer

import (
	"bytes"
	"context"
	"os/exec"
	"strings"

	"go.uber.org/zap"
)

// CommandFactory builds the *exec.Cmd to run. Each package keeps its own variable of this type
// as its test seam and passes it in per call, so swapping it still diverts the subprocess.
type CommandFactory func(ctx context.Context, name string, args ...string) *exec.Cmd

// Command is one composer invocation: run in Dir, under the environment Env guarantees. Shared
// by drush, phpcs and rector too, which all go through `composer exec`.
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
	// Environ() falls back to os.Environ(), layering on the deployment's environment.
	command.Env = append(Env(command.Environ()), c.ExtraEnv...)
	return command
}

// Combined merges stdout and stderr, and strips the output's trailing newline.
func (c Command) Combined(ctx context.Context, args ...string) (string, error) {
	command := c.build(ctx, args...)

	// Hand-rolled because CombinedOutput refuses a command whose Stdout is already set.
	var merged bytes.Buffer
	command.Stdout = &merged
	command.Stderr = &merged
	err := command.Run()

	output := strings.TrimSuffix(merged.String(), "\n")
	c.Logger.Debug(command.String() + "\n" + output)

	return output, err
}

// Split keeps the streams apart, so a PHP notice on stderr cannot corrupt a JSON payload.
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
