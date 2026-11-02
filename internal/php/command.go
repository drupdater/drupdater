// Package php runs composer, which is how drupdater reaches its agent, under the environment
// composer needs.
package php

import (
	"bytes"
	"context"
	"io"
	"os/exec"
	"slices"
	"strings"

	"go.uber.org/zap"
)

// CommandFactory builds the *exec.Cmd to run: exec.CommandContext, or a test's fake.
type CommandFactory func(ctx context.Context, name string, args ...string) *exec.Cmd

// Command is one composer invocation: run in Dir, under the environment Env guarantees.
type Command struct {
	New    CommandFactory
	Logger *zap.Logger
	Dir    string
	// ExtraEnv is appended after Env, so it wins over anything inherited.
	ExtraEnv []string
	Stdin    io.Reader
}

func (c Command) build(ctx context.Context, args ...string) *exec.Cmd {
	command := c.New(ctx, "composer", args...)
	command.Dir = c.Dir
	command.Stdin = c.Stdin
	// Environ() falls back to os.Environ(), layering on the deployment's environment.
	command.Env = append(Env(command.Environ()), c.ExtraEnv...)
	return command
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

// requiredComposerEnv is forced on every invocation (see Env), not left to the Dockerfile, which
// only covers the published image. The 300s default timeout kills a large install mid-phase, and
// the implicit audit is noise in every update's log. Deployment policy is not forced here.
var requiredComposerEnv = []string{
	"COMPOSER_PROCESS_TIMEOUT=0",
	"COMPOSER_NO_AUDIT=1",
}

// Env applies requiredComposerEnv to env, passing everything else through in order — notably
// COMPOSER_AUTH. Inherited assignments to a required variable are dropped rather than shadowed,
// so the outcome does not depend on how os/exec resolves duplicate keys.

func Env(env []string) []string {
	result := make([]string, 0, len(env)+len(requiredComposerEnv))
	for _, entry := range env {
		key, _, isAssignment := strings.Cut(entry, "=")
		// A non-assignment entry cannot override anything, so it is carried over.
		if isAssignment && slices.ContainsFunc(requiredComposerEnv, func(required string) bool {
			requiredKey, _, _ := strings.Cut(required, "=")
			return requiredKey == key
		}) {
			continue
		}
		result = append(result, entry)
	}
	return append(result, requiredComposerEnv...)
}
