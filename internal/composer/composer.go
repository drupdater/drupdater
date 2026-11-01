package composer

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"

	"github.com/drupdater/drupdater/internal/php"

	"github.com/spf13/afero"
	"go.uber.org/zap"
)

type CLI struct {
	fs         afero.Fs
	logger     *zap.Logger
	newCommand php.CommandFactory
	// extraEnv is added to every invocation; see WithoutBlocking.
	extraEnv []string
}

func NewCLI(logger *zap.Logger, opts ...Option) *CLI {
	s := &CLI{
		fs:         afero.NewOsFs(),
		logger:     logger,
		newCommand: exec.CommandContext,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Option configures a CLI.
type Option func(*CLI)

// WithoutBlocking lets composer resolve to versions its policy blocks, such as ones with security
// advisories. A security run needs it: an advisory with no fixed version in range would otherwise
// make every update unresolvable, holding back the fixes that do exist.
func WithoutBlocking() Option {
	return func(s *CLI) {
		s.extraEnv = append(s.extraEnv, "COMPOSER_NO_BLOCKING=1")
	}
}

func (s *CLI) command(dir string) php.Command {
	return php.Command{New: s.newCommand, Logger: s.logger, Dir: dir, ExtraEnv: s.extraEnv}
}

func (s *CLI) execComposer(ctx context.Context, dir string, args ...string) (string, error) {
	return s.command(dir).Combined(ctx, args...)
}

// Versions are the tool versions a run's outcome depends on.
type Versions struct {
	Composer string
	PHP      string
}

// composer --version puts its version on stdout and PHP's on stderr — hence the merged output.
var (
	composerVersionRe = regexp.MustCompile(`(?m)^Composer version (\S+)`)
	phpVersionRe      = regexp.MustCompile(`(?m)^PHP version (\S+)`)
)

// Version reports the composer and PHP versions in play. Not tied to a project, hence no dir.
func (s *CLI) Version(ctx context.Context) (Versions, error) {
	out, err := s.command("").Combined(ctx, "--version", "--no-ansi")
	if err != nil {
		return Versions{}, fmt.Errorf("failed to determine composer version: %w, output: %s", err, out)
	}

	match := composerVersionRe.FindStringSubmatch(out)
	if match == nil {
		return Versions{}, fmt.Errorf("failed to parse composer version from output: %s", out)
	}
	versions := Versions{Composer: match[1]}

	// Composer only started reporting the PHP version in 2.5, so its absence is not an error.
	if phpMatch := phpVersionRe.FindStringSubmatch(out); phpMatch != nil {
		versions.PHP = phpMatch[1]
	}

	return versions, nil
}

func (s *CLI) Install(ctx context.Context, dir string) error {
	out, err := s.execComposer(ctx, dir, "install", "--no-interaction", "--no-progress", "--optimize-autoloader", "--ignore-platform-reqs")
	if err != nil {
		return fmt.Errorf("failed to install dependencies: %w, output: %s", err, out)
	}
	return nil
}

func (s *CLI) Normalize(ctx context.Context, dir string) (string, error) {
	return s.execComposer(ctx, dir, "normalize")
}

func (s *CLI) SetConfig(ctx context.Context, dir string, key string, value string) error {
	_, err := s.execComposer(ctx, dir, "config", "--json", key, value)
	return err
}

func (s *CLI) UnsetConfig(ctx context.Context, dir string, key string) error {
	_, err := s.execComposer(ctx, dir, "config", "--unset", key)
	return err
}

func (s *CLI) UpdateLockHash(ctx context.Context, dir string) error {
	_, err := s.execComposer(ctx, dir, "update", "--lock", "--no-install", "--ignore-platform-reqs")
	return err
}
