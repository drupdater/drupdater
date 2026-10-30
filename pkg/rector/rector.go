package rector

import (
	"context"
	"fmt"
	"github.com/drupdater/drupdater/pkg/php"
	"os/exec"
	"path/filepath"

	"github.com/spf13/afero"
	"go.uber.org/zap"
)

type CLI struct {
	logger     *zap.Logger
	fs         afero.Fs
	newCommand php.CommandFactory
	// binDir holds drupdater's own rector; empty runs the project's through composer.
	binDir string
	config string
}

// NewCLI runs the project's own rector with drupdater's config.
func NewCLI(logger *zap.Logger, config string) *CLI {
	return &CLI{
		logger:     logger,
		fs:         afero.NewOsFs(),
		newCommand: exec.CommandContext,
		config:     config,
	}
}

// NewBundledCLI runs the rector in binDir, which needs nothing installed in the project.
func NewBundledCLI(logger *zap.Logger, binDir string, config string) *CLI {
	return &CLI{
		logger:     logger,
		fs:         afero.NewOsFs(),
		newCommand: exec.CommandContext,
		binDir:     binDir,
		config:     config,
	}
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
			// Equivalent mutant, left un-suppressed: the annotation keys off the enclosing
			// literal and would silence the real FileDiffs and ChangedFiles mutants too.
			Totals: ReturnOutputTotals{
				ChangedFiles: 0,
				Errors:       0,
			},
			FileDiffs:    []ReturnOutputFillDiff{},
			ChangedFiles: []string{},
		}, nil
	}

	command := php.Command{New: s.newCommand, Logger: s.logger, Dir: dir}
	args := []string{"exec", "--", "rector", "process"}
	if s.binDir != "" {
		autoloader, err := projectAutoloader(ctx, command)
		if err != nil {
			return ReturnOutput{}, err
		}
		command.Program = filepath.Join(s.binDir, "rector")
		args = []string{"process", "--autoload-file", autoloader}
	}
	// No --no-diffs: rector's JSON lists each file's applied rules only alongside its diff.
	args = append(args, "--config="+s.config, "--no-progress-bar", "--debug", "--output-format=json")
	args = append(args, customCodeDirectories...)

	deprecationRemovalResult, err := php.JSON[ReturnOutput](ctx, command, args...)
	if err != nil {
		return ReturnOutput{}, fmt.Errorf("failed to run rector: %w", err)
	}

	for _, diff := range deprecationRemovalResult.FileDiffs {
		if err := s.restoreFileDocBlock(dir, diff.File); err != nil {
			return ReturnOutput{}, err
		}
	}

	return deprecationRemovalResult, nil
}

func (s *CLI) restoreFileDocBlock(dir string, file string) error {
	if !filepath.IsAbs(file) {
		file = filepath.Join(dir, file)
	}
	content, err := afero.ReadFile(s.fs, file)
	if err != nil {
		return fmt.Errorf("failed to read %s after rector: %w", file, err)
	}
	fixed := fileDocBlockFirst(string(content))
	if fixed == string(content) {
		return nil
	}
	info, err := s.fs.Stat(file)
	if err != nil {
		return fmt.Errorf("failed to stat %s after rector: %w", file, err)
	}
	if err := afero.WriteFile(s.fs, file, []byte(fixed), info.Mode().Perm()); err != nil {
		return fmt.Errorf("failed to restore the @file docblock in %s: %w", file, err)
	}
	return nil
}

// projectAutoloader honours vendor-dir, where Rector would only ever look under vendor/.
func projectAutoloader(ctx context.Context, composerInDir php.Command) (string, error) {
	vendorDir, _, err := composerInDir.Split(ctx, "config", "--absolute", "vendor-dir")
	if err != nil {
		return "", fmt.Errorf("failed to resolve the project's vendor directory: %w", err)
	}
	return filepath.Join(vendorDir, "autoload.php"), nil
}
