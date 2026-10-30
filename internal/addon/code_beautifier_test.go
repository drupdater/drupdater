package addon

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/drupdater/drupdater/internal/phpcs"
	"github.com/drupdater/drupdater/internal/workflow"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestCreatePHPCSConfig(t *testing.T) {
	logger := zap.NewNop()

	t.Run("Returns error when os.Create fails", func(t *testing.T) {
		composer := NewMockBeautifierComposer(t)
		committer := &fakeCommitter{}

		// Use a path that cannot be written to (root-owned directory)
		const badPath = "/proc/nonexistent"
		composer.EXPECT().GetInstalledPackageVersion(anyCtx, badPath, "drupal/core").Return("10.1.0", nil)
		composer.EXPECT().GetCustomCodeDirectories(anyCtx, badPath).Return([]string{"web/modules/custom"}, nil)

		cb := NewCodeBeautifier(logger, nil, nil, composer)

		_, err := cb.CreatePHPCSConfig(context.Background(), badPath, committer)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create phpcs.xml")
	})

	t.Run("Returns error when committing phpcs.xml fails", func(t *testing.T) {
		composer := NewMockBeautifierComposer(t)
		committer := &fakeCommitter{err: assert.AnError}
		tmpDir := t.TempDir()

		composer.EXPECT().GetInstalledPackageVersion(anyCtx, tmpDir, "drupal/core").Return("10.1.0", nil)
		composer.EXPECT().GetCustomCodeDirectories(anyCtx, tmpDir).Return([]string{"web/modules/custom"}, nil)

		cb := NewCodeBeautifier(logger, nil, nil, composer)

		created, err := cb.CreatePHPCSConfig(context.Background(), tmpDir, committer)
		require.Error(t, err)
		assert.False(t, created)
		require.ErrorIs(t, err, assert.AnError)
	})

	t.Run("Returns false and no error when no custom code directories found", func(t *testing.T) {
		composer := NewMockBeautifierComposer(t)
		committer := &fakeCommitter{}

		tmpDir := t.TempDir()

		composer.EXPECT().GetInstalledPackageVersion(anyCtx, tmpDir, "drupal/core").Return("10.1.0", nil)
		composer.EXPECT().GetCustomCodeDirectories(anyCtx, tmpDir).Return([]string{}, nil)

		cb := NewCodeBeautifier(logger, nil, nil, composer)

		created, err := cb.CreatePHPCSConfig(context.Background(), tmpDir, committer)
		require.NoError(t, err)
		assert.False(t, created)
		assert.Empty(t, committer.commits)

		// No phpcs.xml may be left behind on this path. An empty one would be picked up by the
		// next run as an existing config and fail hasPHPCSPathDefinitions with an XML error.
		assert.NoFileExists(t, filepath.Join(tmpDir, "phpcs.xml"))
	})

	t.Run("Leaves no phpcs.xml behind when GetCustomCodeDirectories fails", func(t *testing.T) {
		composer := NewMockBeautifierComposer(t)
		committer := &fakeCommitter{}

		tmpDir := t.TempDir()

		composer.EXPECT().GetInstalledPackageVersion(anyCtx, tmpDir, "drupal/core").Return("10.1.0", nil)
		composer.EXPECT().GetCustomCodeDirectories(anyCtx, tmpDir).Return(nil, assert.AnError)

		cb := NewCodeBeautifier(logger, nil, nil, composer)

		_, err := cb.CreatePHPCSConfig(context.Background(), tmpDir, committer)
		require.Error(t, err)
		assert.NoFileExists(t, filepath.Join(tmpDir, "phpcs.xml"))
	})

	t.Run("Creates phpcs.xml and commits when custom code directories found", func(t *testing.T) {
		composer := NewMockBeautifierComposer(t)
		committer := &fakeCommitter{}

		tmpDir := t.TempDir()

		composer.EXPECT().GetInstalledPackageVersion(anyCtx, tmpDir, "drupal/core").Return("10.1.0", nil)
		composer.EXPECT().GetCustomCodeDirectories(anyCtx, tmpDir).Return([]string{"web/modules/custom"}, nil)

		cb := NewCodeBeautifier(logger, nil, nil, composer)

		created, err := cb.CreatePHPCSConfig(context.Background(), tmpDir, committer)
		require.NoError(t, err)
		assert.True(t, created)
		assert.Equal(t, []recordedCommit{{message: "Add PHPCS config", paths: []string{"phpcs.xml"}}}, committer.commits)
	})

	t.Run("Returns error when template parsing fails", func(t *testing.T) {
		composer := NewMockBeautifierComposer(t)
		committer := &fakeCommitter{}

		tmpDir := t.TempDir()

		oldTemplate := phpcsTemplateStr
		phpcsTemplateStr = "{{ invalid template syntax"
		defer func() { phpcsTemplateStr = oldTemplate }()

		cb := NewCodeBeautifier(logger, nil, nil, composer)

		_, err := cb.CreatePHPCSConfig(context.Background(), tmpDir, committer)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse phpcs template")
	})

	t.Run("Returns error when template execution fails", func(t *testing.T) {
		composer := NewMockBeautifierComposer(t)
		committer := &fakeCommitter{}

		tmpDir := t.TempDir()

		// Parses fine, but fails at execution: Files is a []string with no such field.
		oldTemplate := phpcsTemplateStr
		phpcsTemplateStr = "{{ .Files.NoSuchField }}"
		defer func() { phpcsTemplateStr = oldTemplate }()

		composer.EXPECT().GetInstalledPackageVersion(anyCtx, tmpDir, "drupal/core").Return("10.1.0", nil)
		composer.EXPECT().GetCustomCodeDirectories(anyCtx, tmpDir).Return([]string{"web/modules/custom"}, nil)

		cb := NewCodeBeautifier(logger, nil, nil, composer)

		_, err := cb.CreatePHPCSConfig(context.Background(), tmpDir, committer)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to execute phpcs template")
		// A failed render must not leave a partially written file for the next run to trip on.
		assert.NoFileExists(t, filepath.Join(tmpDir, "phpcs.xml"))
	})

	t.Run("Returns error when writing phpcs.xml fails", func(t *testing.T) {
		if _, statErr := os.Stat("/dev/full"); os.IsNotExist(statErr) {
			t.Skip("/dev/full not available")
		}

		composer := NewMockBeautifierComposer(t)
		committer := &fakeCommitter{}

		tmpDir := t.TempDir()

		// Symlink phpcs.xml to /dev/full so any write fails with ENOSPC
		if err := os.Symlink("/dev/full", filepath.Join(tmpDir, "phpcs.xml")); err != nil {
			t.Skip("cannot create symlink to /dev/full: " + err.Error())
		}

		composer.EXPECT().GetInstalledPackageVersion(anyCtx, tmpDir, "drupal/core").Return("10.1.0", nil)
		composer.EXPECT().GetCustomCodeDirectories(anyCtx, tmpDir).Return([]string{"web/modules/custom"}, nil)

		cb := NewCodeBeautifier(logger, nil, nil, composer)

		_, err := cb.CreatePHPCSConfig(context.Background(), tmpDir, committer)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to write phpcs.xml")
	})

	t.Run("Returns error when GetCustomCodeDirectories fails", func(t *testing.T) {
		composer := NewMockBeautifierComposer(t)
		committer := &fakeCommitter{}

		tmpDir := t.TempDir()

		composer.EXPECT().GetInstalledPackageVersion(anyCtx, tmpDir, "drupal/core").Return("10.1.0", nil)
		composer.EXPECT().GetCustomCodeDirectories(anyCtx, tmpDir).Return(nil, assert.AnError)

		cb := NewCodeBeautifier(logger, nil, nil, composer)

		_, err := cb.CreatePHPCSConfig(context.Background(), tmpDir, committer)
		require.Error(t, err)
		assert.Equal(t, assert.AnError, err)
	})

}

func TestHasPHPCSPathDefinitions(t *testing.T) {
	t.Run("returns false when neither file exists", func(t *testing.T) {
		result, err := hasPHPCSPathDefinitions("/nonexistent/path")
		require.NoError(t, err)
		assert.False(t, result)
	})

	t.Run("returns true when phpcs.xml has file definitions", func(t *testing.T) {
		tmpDir := t.TempDir()
		content := `<?xml version="1.0" encoding="UTF-8"?>
<ruleset name="test">
    <file>web/modules/custom</file>
</ruleset>`
		err := os.WriteFile(filepath.Join(tmpDir, "phpcs.xml"), []byte(content), 0600)
		require.NoError(t, err)

		result, err := hasPHPCSPathDefinitions(tmpDir)
		require.NoError(t, err)
		assert.True(t, result)
	})

	t.Run("returns false when phpcs.xml has no file definitions", func(t *testing.T) {
		tmpDir := t.TempDir()
		content := `<?xml version="1.0" encoding="UTF-8"?>
<ruleset name="test">
    <rule ref="Drupal"/>
</ruleset>`
		err := os.WriteFile(filepath.Join(tmpDir, "phpcs.xml"), []byte(content), 0600)
		require.NoError(t, err)

		result, err := hasPHPCSPathDefinitions(tmpDir)
		require.NoError(t, err)
		assert.False(t, result)
	})

	t.Run("returns true when phpcs.xml.dist has file definitions", func(t *testing.T) {
		tmpDir := t.TempDir()
		content := `<?xml version="1.0" encoding="UTF-8"?>
<ruleset name="test">
    <file>web/themes/custom</file>
</ruleset>`
		err := os.WriteFile(filepath.Join(tmpDir, "phpcs.xml.dist"), []byte(content), 0600)
		require.NoError(t, err)

		result, err := hasPHPCSPathDefinitions(tmpDir)
		require.NoError(t, err)
		assert.True(t, result)
	})

	t.Run("returns error when phpcs.xml is not valid XML", func(t *testing.T) {
		tmpDir := t.TempDir()
		err := os.WriteFile(filepath.Join(tmpDir, "phpcs.xml"), []byte("not valid xml"), 0600)
		require.NoError(t, err)

		result, err := hasPHPCSPathDefinitions(tmpDir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse phpcs config")
		assert.False(t, result)
	})

	t.Run("returns error when phpcs.xml cannot be read", func(t *testing.T) {
		tmpDir := t.TempDir()
		// Create phpcs.xml as a directory so Stat succeeds but ReadFile fails
		err := os.MkdirAll(filepath.Join(tmpDir, "phpcs.xml"), 0755)
		require.NoError(t, err)

		result, err := hasPHPCSPathDefinitions(tmpDir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to read phpcs config")
		assert.False(t, result)
	})

	t.Run("returns false when phpcs.xml.dist has no file definitions", func(t *testing.T) {
		tmpDir := t.TempDir()
		content := `<?xml version="1.0" encoding="UTF-8"?>
<ruleset name="test">
    <rule ref="Drupal"/>
</ruleset>`
		err := os.WriteFile(filepath.Join(tmpDir, "phpcs.xml.dist"), []byte(content), 0600)
		require.NoError(t, err)

		result, err := hasPHPCSPathDefinitions(tmpDir)
		require.NoError(t, err)
		assert.False(t, result)
	})
}

func TestCodingStyles(t *testing.T) {
	// Create reusable test dependencies
	logger := zap.NewNop()

	// Subtests using table-driven approach
	t.Run("Config file exists but no path definitions", func(t *testing.T) {
		fileExists = func(_ string) bool {
			return true
		}
		oldFn := hasPHPCSPathDefinitions
		hasPHPCSPathDefinitions = func(_ string) (bool, error) {
			return false, nil
		}
		defer func() { hasPHPCSPathDefinitions = oldFn }()

		runner := NewMockPHPCS(t)
		composer := NewMockBeautifierComposer(t)

		committer := &fakeCommitter{}
		updateCodingStyles := NewCodeBeautifier(logger, runner, NewMockPHPCS(t), composer)
		postCodeUpdate := workflow.NewPostCodeUpdateEvent(t.Context(), "/tmp", committer)

		err := updateCodingStyles.postCodeUpdateHandler(postCodeUpdate)
		require.NoError(t, err)

		runner.AssertExpectations(t)
		composer.AssertExpectations(t)
	})

	t.Run("Config file exists but hasPHPCSPathDefinitions returns error", func(t *testing.T) {
		fileExists = func(_ string) bool {
			return true
		}
		oldFn := hasPHPCSPathDefinitions
		hasPHPCSPathDefinitions = func(_ string) (bool, error) {
			return false, assert.AnError
		}
		defer func() { hasPHPCSPathDefinitions = oldFn }()

		runner := NewMockPHPCS(t)
		composer := NewMockBeautifierComposer(t)

		committer := &fakeCommitter{}
		updateCodingStyles := NewCodeBeautifier(logger, runner, NewMockPHPCS(t), composer)
		postCodeUpdate := workflow.NewPostCodeUpdateEvent(t.Context(), "/tmp", committer)

		err := updateCodingStyles.postCodeUpdateHandler(postCodeUpdate)
		require.Error(t, err)
	})

	t.Run("No config file found", func(t *testing.T) {
		// Setup test environment
		fileExists = func(_ string) bool {
			return false
		}

		// Setup mocks with specific expectations
		runner := NewMockPHPCS(t)
		runner.EXPECT().Run(anyCtx, "/tmp").Return(phpcs.ReturnOutput{
			Totals: phpcs.ReturnOutputTotals{
				Errors:   0,
				Warnings: 0,
				Fixable:  0,
			},
			Files: map[string]phpcs.ReturnOutputFile{},
		}, nil)

		composer := NewMockBeautifierComposer(t)
		composer.EXPECT().IsPackageInstalled(anyCtx, "/tmp", "drupal/coder").Return(true, nil)
		composer.EXPECT().GetCustomCodeDirectories(anyCtx, "/tmp").Return([]string{"web/modules/custom", "web/themes/custom"}, nil)
		composer.EXPECT().GetInstalledPackageVersion(anyCtx, "/tmp", "drupal/core").Return("9.2.1", nil)

		// Create system under test
		committer := &fakeCommitter{}
		updateCodingStyles := NewCodeBeautifier(logger, runner, NewMockPHPCS(t), composer)
		postCodeUpdate := workflow.NewPostCodeUpdateEvent(context.Background(), "/tmp", committer)

		// Execute and verify
		err := updateCodingStyles.postCodeUpdateHandler(postCodeUpdate)
		require.NoError(t, err)

		assert.Equal(t, []recordedCommit{{message: "Add PHPCS config", paths: []string{"phpcs.xml"}}}, committer.commits)
		runner.AssertExpectations(t)
		composer.AssertExpectations(t)
	})

	t.Run("without coder in the project, runs the bundled phpcs and leaves composer.* alone", func(t *testing.T) {
		fileExists = func(_ string) bool { return true }
		oldFn := hasPHPCSPathDefinitions
		hasPHPCSPathDefinitions = func(_ string) (bool, error) { return true, nil }
		defer func() { hasPHPCSPathDefinitions = oldFn }()

		bundled := NewMockPHPCS(t)
		bundled.EXPECT().Run(anyCtx, "/tmp").Return(phpcs.ReturnOutput{
			Totals: phpcs.ReturnOutputTotals{Warnings: 1, Fixable: 1},
			Files: map[string]phpcs.ReturnOutputFile{
				"/tmp/web/modules/custom/a.module": {Warnings: 1},
			},
		}, nil)
		bundled.EXPECT().RunCBF(anyCtx, "/tmp").Return(nil)

		composer := NewMockBeautifierComposer(t)
		composer.EXPECT().IsPackageInstalled(anyCtx, "/tmp", "drupal/coder").Return(false, nil)

		committer := &fakeCommitter{}
		cb := NewCodeBeautifier(logger, NewMockPHPCS(t), bundled, composer)
		require.NoError(t, cb.postCodeUpdateHandler(workflow.NewPostCodeUpdateEvent(t.Context(), "/tmp", committer)))

		assert.Equal(t, []recordedCommit{
			{message: "Update coding styles", paths: []string{"web/modules/custom/a.module"}},
		}, committer.commits)
	})

	t.Run("with coder in the project, runs the project's own phpcs", func(t *testing.T) {
		fileExists = func(_ string) bool { return true }
		oldFn := hasPHPCSPathDefinitions
		hasPHPCSPathDefinitions = func(_ string) (bool, error) { return true, nil }
		defer func() { hasPHPCSPathDefinitions = oldFn }()

		project := NewMockPHPCS(t)
		project.EXPECT().Run(anyCtx, "/tmp").Return(phpcs.ReturnOutput{
			Totals: phpcs.ReturnOutputTotals{Fixable: 0},
			Files:  map[string]phpcs.ReturnOutputFile{},
		}, nil)

		composer := NewMockBeautifierComposer(t)
		composer.EXPECT().IsPackageInstalled(anyCtx, "/tmp", "drupal/coder").Return(true, nil)

		committer := &fakeCommitter{}
		cb := NewCodeBeautifier(logger, project, NewMockPHPCS(t), composer)
		require.NoError(t, cb.postCodeUpdateHandler(workflow.NewPostCodeUpdateEvent(t.Context(), "/tmp", committer)))
		assert.Empty(t, committer.commits)
	})

	t.Run("No fixable", func(t *testing.T) {

		fileExists = func(_ string) bool {
			return true
		}
		oldFn := hasPHPCSPathDefinitions
		hasPHPCSPathDefinitions = func(_ string) (bool, error) { return true, nil }
		defer func() { hasPHPCSPathDefinitions = oldFn }()

		runner := NewMockPHPCS(t)
		runner.EXPECT().Run(anyCtx, "/path/to/repo").Return(phpcs.ReturnOutput{
			Totals: phpcs.ReturnOutputTotals{
				Errors:   0,
				Warnings: 0,
				Fixable:  0,
			},
			Files: map[string]phpcs.ReturnOutputFile{},
		}, nil)
		composer := NewMockBeautifierComposer(t)
		composer.EXPECT().IsPackageInstalled(anyCtx, "/path/to/repo", "drupal/coder").Return(true, nil)

		committer := &fakeCommitter{}
		updateCodingStyles := NewCodeBeautifier(logger, runner, NewMockPHPCS(t), composer)
		postCodeUpdate := workflow.NewPostCodeUpdateEvent(t.Context(), "/path/to/repo", committer)
		err := updateCodingStyles.postCodeUpdateHandler(postCodeUpdate)
		require.NoError(t, err)
		runner.AssertExpectations(t)
		composer.AssertExpectations(t)
	})

	t.Run("Fixable", func(t *testing.T) {

		fileExists = func(_ string) bool {
			return true
		}
		oldFn := hasPHPCSPathDefinitions
		hasPHPCSPathDefinitions = func(_ string) (bool, error) { return true, nil }
		defer func() { hasPHPCSPathDefinitions = oldFn }()

		runner := NewMockPHPCS(t)
		runner.EXPECT().Run(anyCtx, "/path/to/repo").Return(phpcs.ReturnOutput{
			Totals: phpcs.ReturnOutputTotals{
				Errors:   0,
				Warnings: 1,
				Fixable:  1,
			},
			Files: map[string]phpcs.ReturnOutputFile{
				"file1.php": {
					Errors:   0,
					Warnings: 1,
					Messages: []phpcs.ReturnOutputFileMessage{
						{
							Message:  "message",
							Source:   "source",
							Severity: 1,
							Fixable:  true,
							Type:     "type",
							Line:     1,
							Column:   1,
						},
					},
				},
			},
		}, nil)
		runner.EXPECT().RunCBF(anyCtx, "/path/to/repo").Return(nil)

		composer := NewMockBeautifierComposer(t)
		composer.EXPECT().IsPackageInstalled(anyCtx, "/path/to/repo", "drupal/coder").Return(true, nil)

		committer := &fakeCommitter{}
		updateCodingStyles := NewCodeBeautifier(logger, runner, NewMockPHPCS(t), composer)
		postCodeUpdate := workflow.NewPostCodeUpdateEvent(t.Context(), "/path/to/repo", committer)
		err := updateCodingStyles.postCodeUpdateHandler(postCodeUpdate)

		require.NoError(t, err)
		assert.Equal(t, []recordedCommit{{message: "Update coding styles", paths: []string{"file1.php"}}}, committer.commits)
		assert.Equal(t, []string{"file1.php"}, updateCodingStyles.fixedFiles)
		runner.AssertExpectations(t)
		composer.AssertExpectations(t)
	})

	t.Run("Fixable but nothing changed reports no fixes", func(t *testing.T) {
		fileExists = func(_ string) bool { return true }
		oldFn := hasPHPCSPathDefinitions
		hasPHPCSPathDefinitions = func(_ string) (bool, error) { return true, nil }
		defer func() { hasPHPCSPathDefinitions = oldFn }()

		runner := NewMockPHPCS(t)
		runner.EXPECT().Run(anyCtx, "/path/to/repo").Return(phpcs.ReturnOutput{
			Totals: phpcs.ReturnOutputTotals{Warnings: 1, Fixable: 1},
			Files: map[string]phpcs.ReturnOutputFile{
				"file1.php": {Warnings: 1, Messages: []phpcs.ReturnOutputFileMessage{{Fixable: true}}},
			},
		}, nil)
		runner.EXPECT().RunCBF(anyCtx, "/path/to/repo").Return(nil)

		composer := NewMockBeautifierComposer(t)
		composer.EXPECT().IsPackageInstalled(anyCtx, "/path/to/repo", "drupal/coder").Return(true, nil)

		committer := &fakeCommitter{unchanged: true}
		updateCodingStyles := NewCodeBeautifier(logger, runner, NewMockPHPCS(t), composer)
		postCodeUpdate := workflow.NewPostCodeUpdateEvent(t.Context(), "/path/to/repo", committer)
		err := updateCodingStyles.postCodeUpdateHandler(postCodeUpdate)

		require.NoError(t, err)
		assert.Empty(t, updateCodingStyles.fixedFiles, "phpcbf changed nothing, so nothing was fixed")
		runner.AssertExpectations(t)
		composer.AssertExpectations(t)
	})

	t.Run("Fixable error", func(t *testing.T) {

		fileExists = func(_ string) bool {
			return true
		}
		oldFn := hasPHPCSPathDefinitions
		hasPHPCSPathDefinitions = func(_ string) (bool, error) { return true, nil }
		defer func() { hasPHPCSPathDefinitions = oldFn }()

		runner := NewMockPHPCS(t)
		runner.EXPECT().Run(anyCtx, "/path/to/repo").Return(phpcs.ReturnOutput{
			Totals: phpcs.ReturnOutputTotals{
				Errors:   0,
				Warnings: 1,
				Fixable:  1,
			},
			Files: map[string]phpcs.ReturnOutputFile{
				"file1.php": {
					Errors:   0,
					Warnings: 1,
					Messages: []phpcs.ReturnOutputFileMessage{
						{
							Message:  "message",
							Source:   "source",
							Severity: 1,
							Fixable:  true,
							Type:     "type",
							Line:     1,
							Column:   1,
						},
					},
				},
			},
		}, assert.AnError)

		composer := NewMockBeautifierComposer(t)
		composer.EXPECT().IsPackageInstalled(anyCtx, "/path/to/repo", "drupal/coder").Return(true, nil)

		committer := &fakeCommitter{}
		updateCodingStyles := NewCodeBeautifier(logger, runner, NewMockPHPCS(t), composer)
		postCodeUpdate := workflow.NewPostCodeUpdateEvent(t.Context(), "/path/to/repo", committer)
		err := updateCodingStyles.postCodeUpdateHandler(postCodeUpdate)

		require.Error(t, err)
		runner.AssertExpectations(t)
		composer.AssertExpectations(t)
	})

}

func TestCodingStyleCommitFailureFailsTheHandler(t *testing.T) {
	fileExists = func(_ string) bool { return true }
	oldFn := hasPHPCSPathDefinitions
	hasPHPCSPathDefinitions = func(_ string) (bool, error) { return true, nil }
	defer func() { hasPHPCSPathDefinitions = oldFn }()

	runner := NewMockPHPCS(t)
	runner.EXPECT().Run(anyCtx, "/path/to/repo").Return(phpcs.ReturnOutput{
		Totals: phpcs.ReturnOutputTotals{Warnings: 1, Fixable: 1},
		Files: map[string]phpcs.ReturnOutputFile{
			"file1.php": {Warnings: 1, Messages: []phpcs.ReturnOutputFileMessage{{Fixable: true}}},
		},
	}, nil)
	runner.EXPECT().RunCBF(anyCtx, "/path/to/repo").Return(nil)
	composer := NewMockBeautifierComposer(t)
	composer.EXPECT().IsPackageInstalled(anyCtx, "/path/to/repo", "drupal/coder").Return(true, nil)

	cb := NewCodeBeautifier(zap.NewNop(), runner, NewMockPHPCS(t), composer)
	err := cb.postCodeUpdateHandler(workflow.NewPostCodeUpdateEvent(t.Context(), "/path/to/repo", &fakeCommitter{err: assert.AnError}))

	require.ErrorIs(t, err, assert.AnError)
	assert.Empty(t, cb.fixedFiles, "fixes that never reached the branch are not reported")
}
