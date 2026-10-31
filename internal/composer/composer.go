package composer

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	"github.com/drupdater/drupdater/internal/drupal"
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

	scratch scratchDir
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

// Exec runs composer with args in dir and returns its combined output: the escape hatch for an
// addon outside drupdater, which the typed methods were not written for.
func (s *CLI) Exec(ctx context.Context, dir string, args ...string) (string, error) {
	return s.execComposer(ctx, dir, args...)
}

// execComposerJSON returns stdout only, so composer's stderr notices cannot corrupt the payload.
func (s *CLI) execComposerJSON(ctx context.Context, dir string, args ...string) (string, error) {
	stdout, _, err := s.command(dir).Split(ctx, args...)
	return stdout, err
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

func (s *CLI) GetInstalledPackageVersion(ctx context.Context, dir string, packageName string) (string, error) {
	composerShow, err := php.JSON[struct {
		Versions []string `json:"versions"`
	}](ctx, s.command(dir), "show", packageName, "--locked", "--no-ansi", "--format=json")
	if err != nil {
		return "", err
	}

	if len(composerShow.Versions) == 0 {
		return "", fmt.Errorf("no versions found for package %s", packageName)
	}
	return composerShow.Versions[0], nil
}

// GetConfig reads a dotted key from composer.json as `composer config --json` prints it: a string
// as is, anything else as JSON. It reads the file as written, so it suits extra.*, not config.*.
func (s *CLI) GetConfig(_ context.Context, dir string, key string) (string, error) {
	content, err := afero.ReadFile(s.fs, dir+"/composer.json")
	if err != nil {
		return "", err
	}
	var value any
	if err := json.Unmarshal(content, &value); err != nil {
		return "", fmt.Errorf("failed to decode composer.json: %w", err)
	}
	for part := range strings.SplitSeq(key, ".") {
		object, ok := value.(map[string]any)
		if !ok {
			return "", fmt.Errorf("%s is not defined", key)
		}
		if value, ok = object[part]; !ok {
			return "", fmt.Errorf("%s is not defined", key)
		}
	}
	if text, ok := value.(string); ok {
		return text, nil
	}
	encoded, err := json.Marshal(value)
	return string(encoded), err
}

func (s *CLI) SetConfig(ctx context.Context, dir string, key string, value string) error {
	_, err := s.execComposer(ctx, dir, "config", "--json", key, value)
	return err
}

func (s *CLI) UnsetConfig(ctx context.Context, dir string, key string) error {
	_, err := s.execComposer(ctx, dir, "config", "--unset", key)
	return err
}

func (s *CLI) IsPackageInstalled(ctx context.Context, dir string, packageToCheck string) (bool, error) {
	_, err := s.execComposer(ctx, dir, "show", "--locked", "--quiet", packageToCheck)
	if err != nil {
		return false, nil //nolint:nilerr // composer show failure means the package is not installed, not an error
	}
	return true, nil
}

func (s *CLI) GetLockHash(dir string) (string, error) {
	file, err := s.fs.Open(dir + "/composer.lock")
	if err != nil {
		return "", err
	}
	defer file.Close()

	var composerLock struct {
		ContentHash string `json:"content-hash"`
	}
	if err := json.NewDecoder(file).Decode(&composerLock); err != nil {
		return "", err
	}

	s.logger.Debug("composer lock hash", zap.String("hash", composerLock.ContentHash))

	return composerLock.ContentHash, nil
}

func (s *CLI) UpdateLockHash(ctx context.Context, dir string) error {
	_, err := s.execComposer(ctx, dir, "update", "--lock", "--no-install", "--ignore-platform-reqs")
	return err
}

func (s *CLI) GetCustomCodeDirectories(ctx context.Context, dir string) ([]string, error) {
	dirs, err := drupal.CustomCodeDirectories(ctx, s.fs, s, dir)
	if err != nil {
		s.logger.Error("failed to get Drupal web dir", zap.String("dir", dir), zap.Error(err))
	}
	return dirs, err
}
