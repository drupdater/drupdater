package services

import (
	"context"
	"errors"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckGitHistoryComplete(t *testing.T) {
	t.Run("a full clone passes", func(t *testing.T) {
		repository := NewMockRepository(t)
		repository.EXPECT().IsShallowClone("/tmp").Return(false, nil)

		result := CheckGitHistoryComplete(repository, "/tmp")
		assert.True(t, result.OK)
		assert.Empty(t, result.Detail)
		// The name is what identifies the check in the output; a passing result with a blank
		// name tells the user nothing.
		assert.NotEmpty(t, result.Name)
	})

	t.Run("a shallow clone fails with an actionable message", func(t *testing.T) {
		repository := NewMockRepository(t)
		repository.EXPECT().IsShallowClone("/tmp").Return(true, nil)

		result := CheckGitHistoryComplete(repository, "/tmp")
		assert.False(t, result.OK)
		assert.NotEmpty(t, result.Name, "a failing check has to say which check failed")
		assert.Contains(t, result.Detail, "shallow checkout detected")
		assert.Contains(t, result.Detail, "fetch-depth")
	})

	t.Run("an error determining depth fails the check", func(t *testing.T) {
		repository := NewMockRepository(t)
		repository.EXPECT().IsShallowClone("/tmp").Return(false, errors.New("open failed"))

		result := CheckGitHistoryComplete(repository, "/tmp")
		assert.False(t, result.OK)
		assert.Contains(t, result.Detail, "open failed")
	})
}

func TestCheckPlatformRequirements(t *testing.T) {
	ctx := context.Background()

	t.Run("satisfied requirements pass", func(t *testing.T) {
		composer := NewMockComposer(t)
		composer.EXPECT().CheckPlatformReqs(ctx, "/tmp").Return("", nil)

		result := CheckPlatformRequirements(ctx, composer, "/tmp")
		assert.True(t, result.OK)
	})

	t.Run("unmet requirements fail with the composer output", func(t *testing.T) {
		composer := NewMockComposer(t)
		composer.EXPECT().CheckPlatformReqs(ctx, "/tmp").Return("php 8.1.0 failed", errors.New("unmet"))

		result := CheckPlatformRequirements(ctx, composer, "/tmp")
		assert.False(t, result.OK)
		assert.Equal(t, "php 8.1.0 failed", result.Detail)
	})
}

func TestCheckSiteSettings(t *testing.T) {
	ctx := context.Background()

	t.Run("settings.php present passes", func(t *testing.T) {
		composer := NewMockComposer(t)
		composer.EXPECT().GetConfig(ctx, "/project", "extra.drupal-scaffold.locations.web-root").Return("web", nil)

		fs := afero.NewMemMapFs()
		require.NoError(t, afero.WriteFile(fs, "/project/web/sites/default/settings.php", []byte("<?php"), 0o644))

		result := CheckSiteSettings(ctx, composer, fs, "/project", "default")
		assert.True(t, result.OK)
	})

	t.Run("a trailing slash on the web root is tolerated", func(t *testing.T) {
		composer := NewMockComposer(t)
		composer.EXPECT().GetConfig(ctx, "/project", "extra.drupal-scaffold.locations.web-root").Return("web/", nil)

		fs := afero.NewMemMapFs()
		require.NoError(t, afero.WriteFile(fs, "/project/web/sites/default/settings.php", []byte("<?php"), 0o644))

		result := CheckSiteSettings(ctx, composer, fs, "/project", "default")
		assert.True(t, result.OK)
	})

	t.Run("a missing settings.php fails with its expected path", func(t *testing.T) {
		composer := NewMockComposer(t)
		composer.EXPECT().GetConfig(ctx, "/project", "extra.drupal-scaffold.locations.web-root").Return("web", nil)

		fs := afero.NewMemMapFs()

		result := CheckSiteSettings(ctx, composer, fs, "/project", "subsite_a")
		assert.False(t, result.OK)
		assert.Contains(t, result.Detail, "web/sites/subsite_a/settings.php")
	})

	t.Run("a composer error surfaces in the detail", func(t *testing.T) {
		composer := NewMockComposer(t)
		composer.EXPECT().GetConfig(ctx, "/project", "extra.drupal-scaffold.locations.web-root").Return("", errors.New("composer.json not found"))

		result := CheckSiteSettings(ctx, composer, afero.NewMemMapFs(), "/project", "default")
		assert.False(t, result.OK)
		assert.Contains(t, result.Detail, "composer.json not found")
	})
}
