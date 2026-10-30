package composer

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/spf13/afero"
	"go.uber.org/zap"
)

var execCommand = exec.CommandContext

type CLI struct {
	fs     afero.Fs
	logger *zap.Logger
	// extraEnv is added to every invocation; see WithoutBlocking.
	extraEnv []string

	tempDir  string
	initOnce sync.Once
	initErr  error
}

func NewCLI(logger *zap.Logger, opts ...Option) *CLI {
	s := &CLI{
		fs:     afero.NewOsFs(),
		logger: logger,
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
//
// Exported because drush, phpcs and rector run through `composer exec`; each should set
// command.Env = composer.Env(command.Environ()).
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

// command is built per call so a test swapping execCommand later still diverts the subprocess.
func (s *CLI) command(dir string) Command {
	return Command{New: execCommand, Logger: s.logger, Dir: dir, ExtraEnv: s.extraEnv}
}

func (s *CLI) execComposer(ctx context.Context, dir string, args ...string) (string, error) {
	return s.command(dir).Combined(ctx, args...)
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
	out, err := s.execComposerJSON(ctx, dir, "show", packageName, "--locked", "--no-ansi", "--format=json")
	if err != nil {
		return "", err
	}

	var composerShow struct {
		Versions []string `json:"versions"`
	}

	if err := json.Unmarshal([]byte(out), &composerShow); err != nil {
		return "", err
	}

	if len(composerShow.Versions) == 0 {
		return "", fmt.Errorf("no versions found for package %s", packageName)
	}
	return composerShow.Versions[0], nil
}

func (s *CLI) GetConfig(ctx context.Context, dir string, key string) (string, error) {
	// stdout only: composer's stderr warnings would corrupt the value.
	return s.execComposerJSON(ctx, dir, "config", "--json", key)
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

// ConfigGetter is the one method WebRoot needs, so a caller's own interface satisfies it.
type ConfigGetter interface {
	GetConfig(ctx context.Context, dir string, key string) (string, error)
}

// WebRoot returns the project's Drupal web root, relative to dir and without a trailing slash.
func WebRoot(ctx context.Context, cfg ConfigGetter, dir string) (string, error) {
	webroot, err := cfg.GetConfig(ctx, dir, "extra.drupal-scaffold.locations.web-root")
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(webroot, "/"), nil
}

func (s *CLI) GetCustomCodeDirectories(ctx context.Context, dir string) ([]string, error) {
	webroot, err := WebRoot(ctx, s, dir)
	if err != nil {
		s.logger.Error("failed to get Drupal web dir", zap.String("dir", dir), zap.Error(err))
		return nil, err
	}

	possibleDirectories := []string{webroot + "/modules/custom", webroot + "/themes/custom", webroot + "/profiles/custom"}
	var customCodeDirectories []string
	for _, possibleDirectory := range possibleDirectories {
		if _, err := s.fs.Stat(dir + "/" + possibleDirectory); os.IsNotExist(err) {
			continue
		}
		customCodeDirectories = append(customCodeDirectories, possibleDirectory)
	}
	return customCodeDirectories, nil
}
