package addon

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"

	"github.com/drupdater/drupdater/internal/services"

	"github.com/go-git/go-git/v5"
	"github.com/gookit/event"
	"go.uber.org/zap"
)

type CodeBeautifier struct {
	logger   *zap.Logger
	phpcs    PHPCS
	composer Composer

	// What the run fixed, for the report. Written once from the single post-code-update
	// event, read after the run.
	fixedFiles []string
	fixable    int
}

func NewCodeBeautifier(logger *zap.Logger, phpcs PHPCS, composer Composer) *CodeBeautifier {
	return &CodeBeautifier{
		logger:   logger,
		phpcs:    phpcs,
		composer: composer,
	}
}

func (cb *CodeBeautifier) SubscribedEvents() map[string]any {
	return map[string]any{
		"post-code-update": event.ListenerItem{
			Priority: event.Normal,
			Listener: event.ListenerFunc(cb.postCodeUpdateHandler),
		},
	}
}

func (cb *CodeBeautifier) RenderTemplate() (string, error) {
	return "", nil
}

// fileExists reports whether path has a phpcs.xml or phpcs.xml.dist.
var fileExists = func(path string) bool {
	if _, err := os.Stat(filepath.Join(path, "phpcs.xml")); os.IsNotExist(err) {
		if _, err := os.Stat(filepath.Join(path, "phpcs.xml.dist")); os.IsNotExist(err) {
			return false
		}
	}
	return true
}

type phpcsRuleset struct {
	XMLName xml.Name `xml:"ruleset"`
	Files   []string `xml:"file"`
}

// hasPHPCSPathDefinitions reports whether the config declares any <file> path.
var hasPHPCSPathDefinitions = func(path string) (bool, error) {
	var configPath string
	if _, err := os.Stat(filepath.Join(path, "phpcs.xml")); err == nil {
		configPath = filepath.Join(path, "phpcs.xml")
	} else if _, err := os.Stat(filepath.Join(path, "phpcs.xml.dist")); err == nil {
		configPath = filepath.Join(path, "phpcs.xml.dist")
	} else {
		return false, nil
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return false, fmt.Errorf("failed to read phpcs config: %w", err)
	}

	var ruleset phpcsRuleset
	if err := xml.Unmarshal(data, &ruleset); err != nil {
		return false, fmt.Errorf("failed to parse phpcs config: %w", err)
	}

	return len(ruleset.Files) > 0, nil
}

func (cb *CodeBeautifier) postCodeUpdateHandler(e event.Event) error { //nolint:cyclop
	event := e.(*services.PostCodeUpdateEvent)
	cb.logger.Info("updating coding styles")

	if !fileExists(event.Path()) {
		created, err := cb.CreatePHPCSConfig(event.Context(), event.Path(), event.Worktree())
		if err != nil {
			return err
		}
		if !created {
			cb.logger.Debug("no phpcs.xml created, skipping coding style update")
			return nil
		}
	} else {
		hasPaths, err := hasPHPCSPathDefinitions(event.Path())
		if err != nil {
			return err
		}
		if !hasPaths {
			cb.logger.Warn("phpcs.xml found but no file path definitions, skipping coding style update")
			return nil
		}
	}

	if installed, _ := cb.composer.IsPackageInstalled(event.Context(), event.Path(), "drupal/coder"); !installed {
		if err := cb.InstallCoder(event.Context(), event.Path(), event.Worktree()); err != nil {
			return err
		}
	}

	codingStyleUpdateResult, err := cb.phpcs.Run(event.Context(), event.Path())
	if err != nil {
		return fmt.Errorf("failed to run phpcs: %w", err)
	}

	if codingStyleUpdateResult.Totals.Fixable == 0 {
		cb.logger.Debug("no coding style issues found")
		return nil
	}

	err = cb.phpcs.RunCBF(event.Context(), event.Path())
	if err != nil {
		cb.logger.Debug("remaining issues", zap.Error(err))
	}

	cb.logger.Debug("adding files to commit", zap.Any("files", codingStyleUpdateResult.Files))

	var addedFiles []string
	for file := range codingStyleUpdateResult.Files {
		if (codingStyleUpdateResult.Files[file].Errors + codingStyleUpdateResult.Files[file].Warnings) == 0 {
			continue
		}
		relativePath := strings.TrimLeft(strings.TrimPrefix(file, event.Path()), "/")

		if _, err := event.Worktree().Add(relativePath); err != nil {
			return fmt.Errorf("failed to add file to commit: %w", err)
		}
		addedFiles = append(addedFiles, relativePath)
	}

	// phpcbf may have changed nothing — not every "fixable" issue is, and RunCBF errors are
	// only logged — which would make an empty commit go-git rejects. Checked against just this
	// handler's own paths, so another listener's dangling change is never swept in.
	staged, err := stagedAnyOf(event.Worktree(), addedFiles)
	if err != nil {
		return fmt.Errorf("failed to check worktree status: %w", err)
	}
	if !staged {
		cb.logger.Debug("no coding style changes to commit")
		return nil
	}

	if _, err := event.Worktree().Commit("Update coding styles", &git.CommitOptions{}); err != nil {
		return err
	}

	sort.Strings(addedFiles)
	cb.fixedFiles = addedFiles
	cb.fixable = codingStyleUpdateResult.Totals.Fixable
	return nil
}

// stagedAnyOf reports whether any of paths has a staged (non-Unmodified) change in worktree. An
// empty paths list is "nothing to check" and short-circuits without querying git status.
func stagedAnyOf(worktree Worktree, paths []string) (bool, error) {
	if len(paths) == 0 {
		return false, nil
	}
	status, err := worktree.Status()
	if err != nil {
		return false, err
	}
	for _, p := range paths {
		if s, ok := status[p]; ok && s.Staging != git.Unmodified {
			return true, nil
		}
	}
	return false, nil
}

var phpcsTemplateStr = `<?xml version="1.0" encoding="UTF-8"?>
<ruleset name="drupal-updater">
    <description>PHP CodeSniffer configuration generated by Drupal Updater</description>
    {{- range .Files }}
    <file>{{ . }}</file>
    {{- end }}
    <arg name="extensions" value="php,module,inc,install,test,profile,theme"/>
    <config name="drupal_core_version" value="{{ .Version }}"/>
    <rule ref="Drupal"/>
    <rule ref="DrupalPractice"/>
</ruleset>
`

func (cb *CodeBeautifier) CreatePHPCSConfig(ctx context.Context, path string, worktree Worktree) (bool, error) {
	cb.logger.Debug("no phpcs.xml or phpcs.xml.dist file found, creating phpcs.xml")

	tmpl, err := template.New("ruleset").Parse(phpcsTemplateStr)
	if err != nil {
		return false, fmt.Errorf("failed to parse phpcs template: %w", err)
	}

	drupalVersion, _ := cb.composer.GetInstalledPackageVersion(ctx, path, "drupal/core")
	majorVersion := strings.Split(drupalVersion, ".")[0]

	data := struct {
		Files   []string
		Version string
	}{
		Files:   []string{},
		Version: majorVersion,
	}

	data.Files, err = cb.composer.GetCustomCodeDirectories(ctx, path)
	if err != nil {
		return false, err
	}

	if len(data.Files) == 0 {
		cb.logger.Debug("no custom code directories found, skipping coding style update")
		return false, nil
	}

	// Render before touching the filesystem: an early return above would otherwise leave an
	// empty phpcs.xml behind, and the next run fails parsing it for <file> definitions.
	var rendered bytes.Buffer
	if err := tmpl.Execute(&rendered, data); err != nil {
		return false, fmt.Errorf("failed to execute phpcs template: %w", err)
	}

	outputFile, err := os.Create(filepath.Join(path, "phpcs.xml"))
	if err != nil {
		return false, fmt.Errorf("failed to create phpcs.xml: %w", err)
	}
	if _, err := outputFile.Write(rendered.Bytes()); err != nil {
		outputFile.Close() // ignore error; the write error takes precedence
		return false, fmt.Errorf("failed to write phpcs.xml: %w", err)
	}
	if err := outputFile.Close(); err != nil {
		return false, fmt.Errorf("failed to close phpcs.xml: %w", err)
	}

	if _, err := worktree.Add("phpcs.xml"); err != nil {
		return false, fmt.Errorf("failed to add file to commit: %w", err)
	}

	if _, err = worktree.Commit("Add PHPCS config", &git.CommitOptions{}); err != nil {
		return false, err
	}

	return true, nil
}

func (cb *CodeBeautifier) InstallCoder(ctx context.Context, path string, worktree Worktree) error {
	cb.logger.Debug("drupal/coder is not installed, installing")
	if _, err := cb.composer.Require(ctx, path, "--dev", "drupal/coder"); err != nil {
		return err
	}

	if err := worktree.AddGlob("composer.*"); err != nil {
		return fmt.Errorf("failed to add file to commit: %w", err)
	}
	if _, err := worktree.Commit("Install drupal/coder", &git.CommitOptions{}); err != nil {
		return err
	}

	return nil
}
