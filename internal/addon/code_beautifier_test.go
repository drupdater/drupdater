package addon

import (
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

		// Use a path that cannot be written to (root-owned directory)
		const badPath = "/proc/nonexistent"
		composer.EXPECT().Project(anyCtx, badPath).Return(withCustomCode(withCore(lockedProject(), "10.1.0"), "web/modules/custom"), nil)

		cb := NewCodeBeautifier(logger, nil, nil, composer)

		_, err := cb.CreatePHPCSConfig(t.Context(), badPath)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create phpcs.xml")
	})

	t.Run("Returns false and no error when no custom code directories found", func(t *testing.T) {
		composer := NewMockBeautifierComposer(t)

		tmpDir := t.TempDir()

		composer.EXPECT().Project(anyCtx, tmpDir).Return(withCustomCode(withCore(lockedProject(), "10.1.0")), nil)

		cb := NewCodeBeautifier(logger, nil, nil, composer)

		created, err := cb.CreatePHPCSConfig(t.Context(), tmpDir)
		require.NoError(t, err)
		assert.False(t, created)

		// No phpcs.xml may be left behind on this path. An empty one would be picked up by the
		// next run as an existing config and fail hasPHPCSPathDefinitions with an XML error.
		assert.NoFileExists(t, filepath.Join(tmpDir, "phpcs.xml"))
	})

	t.Run("Leaves no phpcs.xml behind when GetCustomCodeDirectories fails", func(t *testing.T) {
		composer := NewMockBeautifierComposer(t)

		tmpDir := t.TempDir()

		composer.EXPECT().Project(anyCtx, tmpDir).Return(withCore(lockedProject(), "10.1.0"), assert.AnError)

		cb := NewCodeBeautifier(logger, nil, nil, composer)

		_, err := cb.CreatePHPCSConfig(t.Context(), tmpDir)
		require.Error(t, err)
		assert.NoFileExists(t, filepath.Join(tmpDir, "phpcs.xml"))
	})

	t.Run("Creates phpcs.xml when custom code directories found", func(t *testing.T) {
		composer := NewMockBeautifierComposer(t)

		tmpDir := t.TempDir()

		composer.EXPECT().Project(anyCtx, tmpDir).Return(withCustomCode(withCore(lockedProject(), "10.1.0"), "web/modules/custom"), nil)

		cb := NewCodeBeautifier(logger, nil, nil, composer)

		created, err := cb.CreatePHPCSConfig(t.Context(), tmpDir)
		require.NoError(t, err)
		assert.True(t, created)
		assert.FileExists(t, filepath.Join(tmpDir, "phpcs.xml"))
	})

	t.Run("Returns error when template parsing fails", func(t *testing.T) {
		composer := NewMockBeautifierComposer(t)

		tmpDir := t.TempDir()

		oldTemplate := phpcsTemplateStr
		phpcsTemplateStr = "{{ invalid template syntax"
		defer func() { phpcsTemplateStr = oldTemplate }()

		cb := NewCodeBeautifier(logger, nil, nil, composer)

		_, err := cb.CreatePHPCSConfig(t.Context(), tmpDir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse phpcs template")
	})

	t.Run("Returns error when template execution fails", func(t *testing.T) {
		composer := NewMockBeautifierComposer(t)

		tmpDir := t.TempDir()

		// Parses fine, but fails at execution: Files is a []string with no such field.
		oldTemplate := phpcsTemplateStr
		phpcsTemplateStr = "{{ .Files.NoSuchField }}"
		defer func() { phpcsTemplateStr = oldTemplate }()

		composer.EXPECT().Project(anyCtx, tmpDir).Return(withCustomCode(withCore(lockedProject(), "10.1.0"), "web/modules/custom"), nil)

		cb := NewCodeBeautifier(logger, nil, nil, composer)

		_, err := cb.CreatePHPCSConfig(t.Context(), tmpDir)
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

		tmpDir := t.TempDir()

		// Symlink phpcs.xml to /dev/full so any write fails with ENOSPC
		if err := os.Symlink("/dev/full", filepath.Join(tmpDir, "phpcs.xml")); err != nil {
			t.Skip("cannot create symlink to /dev/full: " + err.Error())
		}

		composer.EXPECT().Project(anyCtx, tmpDir).Return(withCustomCode(withCore(lockedProject(), "10.1.0"), "web/modules/custom"), nil)

		cb := NewCodeBeautifier(logger, nil, nil, composer)

		_, err := cb.CreatePHPCSConfig(t.Context(), tmpDir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to write phpcs.xml")
	})

	t.Run("Returns error when GetCustomCodeDirectories fails", func(t *testing.T) {
		composer := NewMockBeautifierComposer(t)

		tmpDir := t.TempDir()

		composer.EXPECT().Project(anyCtx, tmpDir).Return(withCore(lockedProject(), "10.1.0"), assert.AnError)

		cb := NewCodeBeautifier(logger, nil, nil, composer)

		_, err := cb.CreatePHPCSConfig(t.Context(), tmpDir)
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

		_, err := firePostCodeUpdate(updateCodingStyles, postCodeUpdate)
		require.NoError(t, err)
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

		_, err := firePostCodeUpdate(updateCodingStyles, postCodeUpdate)
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
		composer.EXPECT().Project(anyCtx, "/tmp").Return(withCustomCode(withCore(lockedProject("drupal/coder"), "9.2.1"), "web/modules/custom", "web/themes/custom"), nil)

		// Create system under test
		committer := &fakeCommitter{}
		updateCodingStyles := NewCodeBeautifier(logger, runner, NewMockPHPCS(t), composer)
		postCodeUpdate := workflow.NewPostCodeUpdateEvent(t.Context(), "/tmp", committer)

		// Execute and verify
		_, err := firePostCodeUpdate(updateCodingStyles, postCodeUpdate)
		require.NoError(t, err)

		assert.Equal(t, []recordedCommit{{message: "Add PHPCS config", paths: []string{"phpcs.xml"}}}, committer.commits)
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
		composer.EXPECT().Project(anyCtx, "/tmp").Return(lockedProject(), nil)

		committer := &fakeCommitter{}
		cb := NewCodeBeautifier(logger, NewMockPHPCS(t), bundled, composer)
		_, err := firePostCodeUpdate(cb, workflow.NewPostCodeUpdateEvent(t.Context(), "/tmp", committer))
		require.NoError(t, err)

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
		composer.EXPECT().Project(anyCtx, "/tmp").Return(lockedProject("drupal/coder"), nil)

		committer := &fakeCommitter{}
		cb := NewCodeBeautifier(logger, project, NewMockPHPCS(t), composer)
		_, err := firePostCodeUpdate(cb, workflow.NewPostCodeUpdateEvent(t.Context(), "/tmp", committer))
		require.NoError(t, err)
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
		composer.EXPECT().Project(anyCtx, "/path/to/repo").Return(lockedProject("drupal/coder"), nil)

		committer := &fakeCommitter{}
		updateCodingStyles := NewCodeBeautifier(logger, runner, NewMockPHPCS(t), composer)
		postCodeUpdate := workflow.NewPostCodeUpdateEvent(t.Context(), "/path/to/repo", committer)
		_, err := firePostCodeUpdate(updateCodingStyles, postCodeUpdate)
		require.NoError(t, err)
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
		composer.EXPECT().Project(anyCtx, "/path/to/repo").Return(lockedProject("drupal/coder"), nil)

		committer := &fakeCommitter{}
		updateCodingStyles := NewCodeBeautifier(logger, runner, NewMockPHPCS(t), composer)
		postCodeUpdate := workflow.NewPostCodeUpdateEvent(t.Context(), "/path/to/repo", committer)
		p, err := firePostCodeUpdate(updateCodingStyles, postCodeUpdate)

		require.NoError(t, err)
		assert.Equal(t, []recordedCommit{{message: "Update coding styles", paths: []string{"file1.php"}}}, committer.commits)
		assert.Equal(t, CodingStyleFixes{Files: []string{"file1.php"}, Fixable: 1}, reportSection(p, "code_beautifier"))
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
		composer.EXPECT().Project(anyCtx, "/path/to/repo").Return(lockedProject("drupal/coder"), nil)

		committer := &fakeCommitter{unchanged: true}
		updateCodingStyles := NewCodeBeautifier(logger, runner, NewMockPHPCS(t), composer)
		postCodeUpdate := workflow.NewPostCodeUpdateEvent(t.Context(), "/path/to/repo", committer)
		p, err := firePostCodeUpdate(updateCodingStyles, postCodeUpdate)

		require.NoError(t, err)
		assert.Nil(t, reportSection(p, "code_beautifier"), "phpcbf changed nothing, so nothing was fixed")
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
		composer.EXPECT().Project(anyCtx, "/path/to/repo").Return(lockedProject("drupal/coder"), nil)

		committer := &fakeCommitter{}
		updateCodingStyles := NewCodeBeautifier(logger, runner, NewMockPHPCS(t), composer)
		postCodeUpdate := workflow.NewPostCodeUpdateEvent(t.Context(), "/path/to/repo", committer)
		_, err := firePostCodeUpdate(updateCodingStyles, postCodeUpdate)

		require.ErrorIs(t, err, assert.AnError)
		assert.Contains(t, err.Error(), "failed to run phpcs")
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
	composer.EXPECT().Project(anyCtx, "/path/to/repo").Return(lockedProject("drupal/coder"), nil)

	cb := NewCodeBeautifier(zap.NewNop(), runner, NewMockPHPCS(t), composer)
	p, err := firePostCodeUpdate(cb, workflow.NewPostCodeUpdateEvent(t.Context(), "/path/to/repo", &fakeCommitter{err: assert.AnError}))

	require.ErrorIs(t, err, assert.AnError)
	assert.Nil(t, reportSection(p, "code_beautifier"), "fixes that never reached the branch are not reported")
}

func TestCodingStylesCommitTheCreatedConfigBeforeTheFixes(t *testing.T) {
	fileExists = func(_ string) bool { return false }
	tmpDir := t.TempDir()

	runner := NewMockPHPCS(t)
	runner.EXPECT().Run(anyCtx, tmpDir).Return(phpcs.ReturnOutput{
		Totals: phpcs.ReturnOutputTotals{Warnings: 1, Fixable: 1},
		Files: map[string]phpcs.ReturnOutputFile{
			tmpDir + "/web/modules/custom/b.module":     {Warnings: 1},
			tmpDir + "/web/modules/custom/a.module":     {Errors: 1},
			tmpDir + "/web/modules/custom/clean.module": {},
		},
	}, nil)
	runner.EXPECT().RunCBF(anyCtx, tmpDir).Return(assert.AnError)
	composer := NewMockBeautifierComposer(t)
	composer.EXPECT().Project(anyCtx, tmpDir).Return(withCustomCode(withCore(lockedProject("drupal/coder"), "10.1.0"), "web/modules/custom"), nil)

	committer := &fakeCommitter{}
	_, err := firePostCodeUpdate(NewCodeBeautifier(zap.NewNop(), runner, NewMockPHPCS(t), composer), workflow.NewPostCodeUpdateEvent(t.Context(), tmpDir, committer))

	require.NoError(t, err, "phpcbf exits non-zero whenever issues remain")
	assert.Equal(t, []recordedCommit{
		{message: "Add PHPCS config", paths: []string{"phpcs.xml"}},
		{message: "Update coding styles", paths: []string{"web/modules/custom/a.module", "web/modules/custom/b.module"}},
	}, committer.commits)
}

func TestCodingStylesFailWhenTheCreatedConfigCannotBeCommitted(t *testing.T) {
	fileExists = func(_ string) bool { return false }
	tmpDir := t.TempDir()

	runner := NewMockPHPCS(t)
	runner.EXPECT().Run(anyCtx, tmpDir).Return(phpcs.ReturnOutput{}, nil)
	composer := NewMockBeautifierComposer(t)
	composer.EXPECT().Project(anyCtx, tmpDir).Return(withCustomCode(withCore(lockedProject("drupal/coder"), "10.1.0"), "web/modules/custom"), nil)

	_, err := firePostCodeUpdate(NewCodeBeautifier(zap.NewNop(), runner, NewMockPHPCS(t), composer),
		workflow.NewPostCodeUpdateEvent(t.Context(), tmpDir, &fakeCommitter{err: assert.AnError}))

	require.ErrorIs(t, err, assert.AnError)
}

func TestCodingStylesFailWhenTheConfigCannotBeCreated(t *testing.T) {
	fileExists = func(_ string) bool { return false }
	composer := NewMockBeautifierComposer(t)
	composer.EXPECT().Project(anyCtx, "/tmp").Return(withCore(lockedProject(), "10.1.0"), assert.AnError)

	committer := &fakeCommitter{}
	_, err := firePostCodeUpdate(NewCodeBeautifier(zap.NewNop(), NewMockPHPCS(t), NewMockPHPCS(t), composer),
		workflow.NewPostCodeUpdateEvent(t.Context(), "/tmp", committer))

	require.ErrorIs(t, err, assert.AnError)
	assert.Empty(t, committer.commits)
}
