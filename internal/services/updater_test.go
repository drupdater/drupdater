package services

import (
	"testing"

	internal "github.com/drupdater/drupdater/internal"
	"github.com/drupdater/drupdater/pkg/composer"
	"github.com/drupdater/drupdater/pkg/drupal"
	"github.com/drupdater/drupdater/pkg/drupalorg"
	"github.com/drupdater/drupdater/pkg/drush"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.uber.org/zap"
)

func TestUpdateDependencies(t *testing.T) {

	logger := zap.NewNop()
	t.Run("Update without patches and plugins", func(t *testing.T) {

		composerService := composer.NewMockRunner(t)
		drupalOrgService := drupalorg.NewMockClient(t)
		worktree := internal.NewMockWorktree(t)

		composerService.On("Update", mock.Anything, "/tmp", []string{}, []string{}, false, false).Return([]composer.PackageChange{
			{
				Package: "drupal/core",
				Action:  "update",
				From:    "9.4.0",
				To:      "9.4.1",
			},
		}, nil)

		worktree.On("AddGlob", "composer.*").Return(nil)
		worktree.On("Commit", "Update composer.json and composer.lock", &git.CommitOptions{}).Return(plumbing.NewHash(""), nil)

		updater := &DefaultUpdater{
			logger:   logger,
			composer: composerService,
		}

		err := updater.UpdateDependencies(t.Context(), "/tmp", []string{}, worktree, false)

		composerService.AssertExpectations(t)
		drupalOrgService.AssertExpectations(t)
		worktree.AssertExpectations(t)

		assert.NoError(t, err)
	})
}

func TestUpdateDrupal(t *testing.T) {

	logger := zap.NewNop()

	t.Run("Update drupal", func(t *testing.T) {

		worktree := internal.NewMockWorktree(t)
		settingsService := drupal.NewMockSettingsService(t)
		drushService := drush.NewMockRunner(t)

		settingsService.On("ConfigureDatabase", mock.Anything, "/tmp", "site1").Return(nil)

		drushService.On("UpdateSite", mock.Anything, "/tmp", "site1").Return(nil)
		drushService.On("ConfigResave", mock.Anything, "/tmp", "site1").Return(nil)
		drushService.On("ExportConfiguration", mock.Anything, "/tmp", "site1").Return(nil)

		updater := &DefaultUpdater{
			logger:   logger,
			settings: settingsService,
			drush:    drushService,
		}

		err := updater.UpdateDrupal(t.Context(), "/tmp", worktree, "site1")

		settingsService.AssertExpectations(t)
		worktree.AssertExpectations(t)

		assert.NoError(t, err)
	})

}
