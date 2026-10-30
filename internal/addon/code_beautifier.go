package addon

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"text/template"

	"github.com/drupdater/drupdater/internal/workflow"
	"github.com/drupdater/drupdater/pkg/phpcs"

	"go.uber.org/zap"
)

// BeautifierComposer finds custom code and which coding standards the project installs.
type BeautifierComposer interface {
	GetCustomCodeDirectories(ctx context.Context, dir string) ([]string, error)
	GetInstalledPackageVersion(ctx context.Context, dir string, packageName string) (string, error)
	IsPackageInstalled(ctx context.Context, dir string, packageToCheck string) (bool, error)
}

// PHPCS runs phpcs and phpcbf.
type PHPCS interface {
	Run(ctx context.Context, path string) (phpcs.ReturnOutput, error)
	RunCBF(ctx context.Context, path string) error
}

type CodeBeautifier struct {
	logger       *zap.Logger
	projectPHPCS PHPCS
	bundledPHPCS PHPCS
	composer     BeautifierComposer

	// For the report. Written once from the single post-code-update event, read after the run.
	fixedFiles []string
	fixable    int
}

// NewCodeBeautifier runs projectPHPCS when the project has drupal/coder, bundledPHPCS otherwise.
func NewCodeBeautifier(logger *zap.Logger, projectPHPCS PHPCS, bundledPHPCS PHPCS, composer BeautifierComposer) *CodeBeautifier {
	return &CodeBeautifier{
		logger:       logger,
		projectPHPCS: projectPHPCS,
		bundledPHPCS: bundledPHPCS,
		composer:     composer,
	}
}

func (cb *CodeBeautifier) Hooks() workflow.Hooks {
	return workflow.Hooks{
		PostCodeUpdate: workflow.On(workflow.Format, cb.postCodeUpdateHandler),
	}
}

// phpcsConfigPath returns the project's phpcs config file. An unreadable config counts as found:
// only absence means "no config".
func phpcsConfigPath(path string) (configPath string, found bool) {
	for _, name := range []string{"phpcs.xml", "phpcs.xml.dist"} {
		candidate := filepath.Join(path, name)
		if _, err := os.Stat(candidate); !os.IsNotExist(err) {
			return candidate, true
		}
	}
	return "", false
}

// fileExists reports whether path has a phpcs.xml or phpcs.xml.dist.
var fileExists = func(path string) bool {
	_, found := phpcsConfigPath(path)
	return found
}

type phpcsRuleset struct {
	XMLName xml.Name `xml:"ruleset"`
	Files   []string `xml:"file"`
}

// hasPHPCSPathDefinitions reports whether the config declares any <file> path.
var hasPHPCSPathDefinitions = func(path string) (bool, error) {
	configPath, found := phpcsConfigPath(path)
	if !found {
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

func (cb *CodeBeautifier) postCodeUpdateHandler(evt *workflow.PostCodeUpdateEvent) error { //nolint:cyclop
	cb.logger.Info("updating coding styles")

	if !fileExists(evt.Path()) {
		created, err := cb.CreatePHPCSConfig(evt.Context(), evt.Path(), evt)
		if err != nil {
			return err
		}
		if !created {
			cb.logger.Debug("no phpcs.xml created, skipping coding style update")
			return nil
		}
	} else {
		hasPaths, err := hasPHPCSPathDefinitions(evt.Path())
		if err != nil {
			return err
		}
		if !hasPaths {
			cb.logger.Warn("phpcs.xml found but no file path definitions, skipping coding style update")
			return nil
		}
	}

	// The project's own coder brings the version and extra standards its phpcs.xml was written for.
	phpcs := cb.bundledPHPCS
	if installed, _ := cb.composer.IsPackageInstalled(evt.Context(), evt.Path(), "drupal/coder"); installed {
		phpcs = cb.projectPHPCS
	}

	codingStyleUpdateResult, err := phpcs.Run(evt.Context(), evt.Path())
	if err != nil {
		return fmt.Errorf("failed to run phpcs: %w", err)
	}

	if codingStyleUpdateResult.Totals.Fixable == 0 {
		cb.logger.Debug("no coding style issues found")
		return nil
	}

	err = phpcs.RunCBF(evt.Context(), evt.Path())
	if err != nil {
		cb.logger.Debug("remaining issues", zap.Error(err))
	}

	cb.logger.Debug("adding files to commit", zap.Any("files", codingStyleUpdateResult.Files))

	var addedFiles []string
	for file := range codingStyleUpdateResult.Files {
		if (codingStyleUpdateResult.Files[file].Errors + codingStyleUpdateResult.Files[file].Warnings) == 0 {
			continue
		}
		addedFiles = append(addedFiles, strings.TrimLeft(strings.TrimPrefix(file, evt.Path()), "/"))
	}

	// phpcbf may have changed nothing even where phpcs reported something fixable.
	committed, err := evt.Commit("Update coding styles", addedFiles...)
	if err != nil {
		return err
	}
	if !committed {
		cb.logger.Debug("no coding style changes to commit")
		return nil
	}

	slices.Sort(addedFiles)
	cb.fixedFiles = addedFiles
	cb.fixable = codingStyleUpdateResult.Totals.Fixable
	return nil
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

func (cb *CodeBeautifier) CreatePHPCSConfig(ctx context.Context, path string, committer workflow.Committer) (bool, error) {
	cb.logger.Debug("no phpcs.xml or phpcs.xml.dist file found, creating phpcs.xml")

	tmpl, err := template.New("ruleset").Parse(phpcsTemplateStr)
	if err != nil {
		return false, fmt.Errorf("failed to parse phpcs template: %w", err)
	}

	drupalVersion, _ := cb.composer.GetInstalledPackageVersion(ctx, path, "drupal/core")
	majorVersion, _, _ := strings.Cut(drupalVersion, ".")

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

	// Render before touching the filesystem, or a template failure leaves an empty phpcs.xml
	// behind that the next run fails to parse.
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

	if _, err := committer.Commit("Add PHPCS config", "phpcs.xml"); err != nil {
		return false, err
	}

	return true, nil
}
