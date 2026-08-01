package addon

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/drupdater/drupdater/internal/services"
	"github.com/drupdater/drupdater/pkg/phpcs"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/gookit/event"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestCodeBeautifier_SubscribedEvents(t *testing.T) {
	cb := &CodeBeautifier{}
	events := cb.SubscribedEvents()

	assert.Contains(t, events, "post-code-update")
	item := events["post-code-update"].(event.ListenerItem)
	assert.Equal(t, event.Normal, item.Priority)
}

func TestCodeBeautifier_RenderTemplate(t *testing.T) {
	cb := &CodeBeautifier{}
	result, err := cb.RenderTemplate()
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestCreatePHPCSConfig(t *testing.T) {
	logger := zap.NewNop()

	t.Run("Returns error when os.Create fails", func(t *testing.T) {
		composer := NewMockComposer(t)
		worktree := NewMockWorktree(t)

		// Use a path that cannot be written to (root-owned directory)
		const badPath = "/proc/nonexistent"
		composer.EXPECT().GetInstalledPackageVersion(anyCtx, badPath, "drupal/core").Return("10.1.0", nil)
		composer.EXPECT().GetCustomCodeDirectories(anyCtx, badPath).Return([]string{"web/modules/custom"}, nil)

		cb := NewCodeBeautifier(logger, nil, composer)

		_, err := cb.CreatePHPCSConfig(context.Background(), badPath, worktree)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create phpcs.xml")
	})

	t.Run("Returns false and no error when no custom code directories found", func(t *testing.T) {
		composer := NewMockComposer(t)
		worktree := NewMockWorktree(t)

		tmpDir := t.TempDir()

		composer.EXPECT().GetInstalledPackageVersion(anyCtx, tmpDir, "drupal/core").Return("10.1.0", nil)
		composer.EXPECT().GetCustomCodeDirectories(anyCtx, tmpDir).Return([]string{}, nil)

		cb := NewCodeBeautifier(logger, nil, composer)

		created, err := cb.CreatePHPCSConfig(context.Background(), tmpDir, worktree)
		require.NoError(t, err)
		assert.False(t, created)

		// No phpcs.xml may be left behind on this path. An empty one would be picked up by the
		// next run as an existing config and fail hasPHPCSPathDefinitions with an XML error.
		assert.NoFileExists(t, filepath.Join(tmpDir, "phpcs.xml"))
	})

	t.Run("Leaves no phpcs.xml behind when GetCustomCodeDirectories fails", func(t *testing.T) {
		composer := NewMockComposer(t)
		worktree := NewMockWorktree(t)

		tmpDir := t.TempDir()

		composer.EXPECT().GetInstalledPackageVersion(anyCtx, tmpDir, "drupal/core").Return("10.1.0", nil)
		composer.EXPECT().GetCustomCodeDirectories(anyCtx, tmpDir).Return(nil, assert.AnError)

		cb := NewCodeBeautifier(logger, nil, composer)

		_, err := cb.CreatePHPCSConfig(context.Background(), tmpDir, worktree)
		require.Error(t, err)
		assert.NoFileExists(t, filepath.Join(tmpDir, "phpcs.xml"))
	})

	t.Run("Creates phpcs.xml and commits when custom code directories found", func(t *testing.T) {
		composer := NewMockComposer(t)
		worktree := NewMockWorktree(t)

		tmpDir := t.TempDir()

		composer.EXPECT().GetInstalledPackageVersion(anyCtx, tmpDir, "drupal/core").Return("10.1.0", nil)
		composer.EXPECT().GetCustomCodeDirectories(anyCtx, tmpDir).Return([]string{"web/modules/custom"}, nil)
		worktree.EXPECT().Add("phpcs.xml").Return(plumbing.NewHash(""), nil)
		worktree.EXPECT().Commit("Add PHPCS config", &git.CommitOptions{}).Return(plumbing.NewHash(""), nil)

		cb := NewCodeBeautifier(logger, nil, composer)

		created, err := cb.CreatePHPCSConfig(context.Background(), tmpDir, worktree)
		require.NoError(t, err)
		assert.True(t, created)
	})

	t.Run("Returns error when template parsing fails", func(t *testing.T) {
		composer := NewMockComposer(t)
		worktree := NewMockWorktree(t)

		tmpDir := t.TempDir()

		oldTemplate := phpcsTemplateStr
		phpcsTemplateStr = "{{ invalid template syntax"
		defer func() { phpcsTemplateStr = oldTemplate }()

		cb := NewCodeBeautifier(logger, nil, composer)

		_, err := cb.CreatePHPCSConfig(context.Background(), tmpDir, worktree)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse phpcs template")
	})

	t.Run("Returns error when template execution fails", func(t *testing.T) {
		composer := NewMockComposer(t)
		worktree := NewMockWorktree(t)

		tmpDir := t.TempDir()

		// Parses fine, but fails at execution: Files is a []string with no such field.
		oldTemplate := phpcsTemplateStr
		phpcsTemplateStr = "{{ .Files.NoSuchField }}"
		defer func() { phpcsTemplateStr = oldTemplate }()

		composer.EXPECT().GetInstalledPackageVersion(anyCtx, tmpDir, "drupal/core").Return("10.1.0", nil)
		composer.EXPECT().GetCustomCodeDirectories(anyCtx, tmpDir).Return([]string{"web/modules/custom"}, nil)

		cb := NewCodeBeautifier(logger, nil, composer)

		_, err := cb.CreatePHPCSConfig(context.Background(), tmpDir, worktree)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to execute phpcs template")
		// A failed render must not leave a partially written file for the next run to trip on.
		assert.NoFileExists(t, filepath.Join(tmpDir, "phpcs.xml"))
	})

	t.Run("Returns error when writing phpcs.xml fails", func(t *testing.T) {
		if _, statErr := os.Stat("/dev/full"); os.IsNotExist(statErr) {
			t.Skip("/dev/full not available")
		}

		composer := NewMockComposer(t)
		worktree := NewMockWorktree(t)

		tmpDir := t.TempDir()

		// Symlink phpcs.xml to /dev/full so any write fails with ENOSPC
		if err := os.Symlink("/dev/full", filepath.Join(tmpDir, "phpcs.xml")); err != nil {
			t.Skip("cannot create symlink to /dev/full: " + err.Error())
		}

		composer.EXPECT().GetInstalledPackageVersion(anyCtx, tmpDir, "drupal/core").Return("10.1.0", nil)
		composer.EXPECT().GetCustomCodeDirectories(anyCtx, tmpDir).Return([]string{"web/modules/custom"}, nil)

		cb := NewCodeBeautifier(logger, nil, composer)

		_, err := cb.CreatePHPCSConfig(context.Background(), tmpDir, worktree)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to write phpcs.xml")
	})

	t.Run("Returns error when GetCustomCodeDirectories fails", func(t *testing.T) {
		composer := NewMockComposer(t)
		worktree := NewMockWorktree(t)

		tmpDir := t.TempDir()

		composer.EXPECT().GetInstalledPackageVersion(anyCtx, tmpDir, "drupal/core").Return("10.1.0", nil)
		composer.EXPECT().GetCustomCodeDirectories(anyCtx, tmpDir).Return(nil, assert.AnError)

		cb := NewCodeBeautifier(logger, nil, composer)

		_, err := cb.CreatePHPCSConfig(context.Background(), tmpDir, worktree)
		require.Error(t, err)
		assert.Equal(t, assert.AnError, err)
	})

	t.Run("Returns error when worktree.Add fails", func(t *testing.T) {
		composer := NewMockComposer(t)
		worktree := NewMockWorktree(t)

		tmpDir := t.TempDir()

		composer.EXPECT().GetInstalledPackageVersion(anyCtx, tmpDir, "drupal/core").Return("10.1.0", nil)
		composer.EXPECT().GetCustomCodeDirectories(anyCtx, tmpDir).Return([]string{"web/modules/custom"}, nil)
		worktree.EXPECT().Add("phpcs.xml").Return(plumbing.NewHash(""), assert.AnError)

		cb := NewCodeBeautifier(logger, nil, composer)

		_, err := cb.CreatePHPCSConfig(context.Background(), tmpDir, worktree)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to add file to commit")
	})

	t.Run("Returns error when worktree.Commit fails", func(t *testing.T) {
		composer := NewMockComposer(t)
		worktree := NewMockWorktree(t)

		tmpDir := t.TempDir()

		composer.EXPECT().GetInstalledPackageVersion(anyCtx, tmpDir, "drupal/core").Return("10.1.0", nil)
		composer.EXPECT().GetCustomCodeDirectories(anyCtx, tmpDir).Return([]string{"web/modules/custom"}, nil)
		worktree.EXPECT().Add("phpcs.xml").Return(plumbing.NewHash(""), nil)
		worktree.EXPECT().Commit("Add PHPCS config", &git.CommitOptions{}).Return(plumbing.NewHash(""), assert.AnError)

		cb := NewCodeBeautifier(logger, nil, composer)

		_, err := cb.CreatePHPCSConfig(context.Background(), tmpDir, worktree)
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
	worktree := NewMockWorktree(t)

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
		composer := NewMockComposer(t)

		updateCodingStyles := NewCodeBeautifier(logger, runner, composer)
		postCodeUpdate := services.NewPostCodeUpdateEvent(t.Context(), "/tmp", worktree)

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
		composer := NewMockComposer(t)

		updateCodingStyles := NewCodeBeautifier(logger, runner, composer)
		postCodeUpdate := services.NewPostCodeUpdateEvent(t.Context(), "/tmp", worktree)

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

		composer := NewMockComposer(t)
		composer.EXPECT().IsPackageInstalled(anyCtx, "/tmp", "drupal/coder").Return(true, nil)
		composer.EXPECT().GetCustomCodeDirectories(anyCtx, "/tmp").Return([]string{"web/modules/custom", "web/themes/custom"}, nil)
		composer.EXPECT().GetInstalledPackageVersion(anyCtx, "/tmp", "drupal/core").Return("9.2.1", nil)

		worktree.EXPECT().Add("phpcs.xml").Return(plumbing.NewHash(""), nil)
		worktree.EXPECT().Commit("Add PHPCS config", &git.CommitOptions{}).Return(plumbing.NewHash(""), nil)

		// Create system under test
		updateCodingStyles := NewCodeBeautifier(logger, runner, composer)
		postCodeUpdate := services.NewPostCodeUpdateEvent(context.Background(), "/tmp", worktree)

		// Execute and verify
		err := updateCodingStyles.postCodeUpdateHandler(postCodeUpdate)
		require.NoError(t, err)

		// Verify all expectations were met
		runner.AssertExpectations(t)
		composer.AssertExpectations(t)
	})

	t.Run("No coder found", func(t *testing.T) {

		fileExists = func(_ string) bool {
			return true
		}
		oldFn := hasPHPCSPathDefinitions
		hasPHPCSPathDefinitions = func(_ string) (bool, error) { return true, nil }
		defer func() { hasPHPCSPathDefinitions = oldFn }()

		runner := NewMockPHPCS(t)
		runner.EXPECT().Run(anyCtx, "/tmp").Return(phpcs.ReturnOutput{
			Totals: phpcs.ReturnOutputTotals{
				Errors:   0,
				Warnings: 0,
				Fixable:  0,
			},
			Files: map[string]phpcs.ReturnOutputFile{},
		}, nil)

		composer := NewMockComposer(t)
		composer.EXPECT().IsPackageInstalled(anyCtx, "/tmp", "drupal/coder").Return(false, nil)
		composer.EXPECT().Require(anyCtx, "/tmp", []string{"--dev", "drupal/coder"}).Return("", nil)

		worktree.EXPECT().AddGlob("composer.*").Return(nil)
		worktree.EXPECT().Commit("Install drupal/coder", &git.CommitOptions{}).Return(plumbing.NewHash(""), nil)

		updateCodingStyles := NewCodeBeautifier(logger, runner, composer)
		postCodeUpdate := services.NewPostCodeUpdateEvent(t.Context(), "/tmp", worktree)
		err := updateCodingStyles.postCodeUpdateHandler(postCodeUpdate)
		require.NoError(t, err)
		runner.AssertExpectations(t)
		composer.AssertExpectations(t)
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
		composer := NewMockComposer(t)
		composer.EXPECT().IsPackageInstalled(anyCtx, "/path/to/repo", "drupal/coder").Return(true, nil)

		updateCodingStyles := NewCodeBeautifier(logger, runner, composer)
		postCodeUpdate := services.NewPostCodeUpdateEvent(t.Context(), "/path/to/repo", worktree)
		err := updateCodingStyles.postCodeUpdateHandler(postCodeUpdate)
		require.NoError(t, err)
		runner.AssertExpectations(t)
		composer.AssertExpectations(t)
	})

	t.Run("stagedAnyOf reports staged changes and propagates errors, scoped to given paths", func(t *testing.T) {
		wt := NewMockWorktree(t)
		wt.EXPECT().Status().Return(git.Status{"a": &git.FileStatus{Staging: git.Unmodified}}, nil).Once()
		staged, err := stagedAnyOf(wt, []string{"a"})
		require.NoError(t, err)
		assert.False(t, staged)

		wt.EXPECT().Status().Return(git.Status{"a": &git.FileStatus{Staging: git.Modified}}, nil).Once()
		staged, err = stagedAnyOf(wt, []string{"a"})
		require.NoError(t, err)
		assert.True(t, staged)

		// A staged change to a path the caller didn't ask about must not count.
		wt.EXPECT().Status().Return(git.Status{"b": &git.FileStatus{Staging: git.Modified}}, nil).Once()
		staged, err = stagedAnyOf(wt, []string{"a"})
		require.NoError(t, err)
		assert.False(t, staged)

		wt.EXPECT().Status().Return(nil, assert.AnError).Once()
		_, err = stagedAnyOf(wt, []string{"a"})
		require.Error(t, err)

		// An empty path list short-circuits without even querying git status.
		staged, err = stagedAnyOf(wt, nil)
		require.NoError(t, err)
		assert.False(t, staged)
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

		composer := NewMockComposer(t)
		composer.EXPECT().IsPackageInstalled(anyCtx, "/path/to/repo", "drupal/coder").Return(true, nil)

		worktree.EXPECT().Add("file1.php").Return(plumbing.NewHash(""), nil)
		worktree.EXPECT().Status().Return(git.Status{"file1.php": &git.FileStatus{Staging: git.Modified}}, nil)
		worktree.EXPECT().Commit("Update coding styles", &git.CommitOptions{}).Return(plumbing.NewHash(""), nil)

		updateCodingStyles := NewCodeBeautifier(logger, runner, composer)
		postCodeUpdate := services.NewPostCodeUpdateEvent(t.Context(), "/path/to/repo", worktree)
		err := updateCodingStyles.postCodeUpdateHandler(postCodeUpdate)

		require.NoError(t, err)
		runner.AssertExpectations(t)
		composer.AssertExpectations(t)
	})

	t.Run("Fixable but nothing staged skips the commit", func(t *testing.T) {
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

		composer := NewMockComposer(t)
		composer.EXPECT().IsPackageInstalled(anyCtx, "/path/to/repo", "drupal/coder").Return(true, nil)

		worktree.EXPECT().Add("file1.php").Return(plumbing.NewHash(""), nil)
		worktree.EXPECT().Status().Return(git.Status{}, nil)
		// No Commit expectation: an empty index must not trigger a commit.

		updateCodingStyles := NewCodeBeautifier(logger, runner, composer)
		postCodeUpdate := services.NewPostCodeUpdateEvent(t.Context(), "/path/to/repo", worktree)
		err := updateCodingStyles.postCodeUpdateHandler(postCodeUpdate)

		require.NoError(t, err)
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

		composer := NewMockComposer(t)
		composer.EXPECT().IsPackageInstalled(anyCtx, "/path/to/repo", "drupal/coder").Return(true, nil)

		updateCodingStyles := NewCodeBeautifier(logger, runner, composer)
		postCodeUpdate := services.NewPostCodeUpdateEvent(t.Context(), "/path/to/repo", worktree)
		err := updateCodingStyles.postCodeUpdateHandler(postCodeUpdate)

		require.Error(t, err)
		runner.AssertExpectations(t)
		composer.AssertExpectations(t)
	})

}
