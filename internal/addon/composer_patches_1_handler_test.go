package addon

import (
	"context"
	"errors"
	"testing"

	"github.com/drupdater/drupdater/internal/services"
	"github.com/drupdater/drupdater/pkg/composer"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.uber.org/zap"
)

func TestPreComposerUpdateHandler(t *testing.T) {
	logger := zap.NewNop()
	path := "/repo"

	t.Run("persists and commits when a patch is removed", func(t *testing.T) {
		const depPatch = "https://www.drupal.org/files/issues/x.patch"

		composerService := NewMockComposer(t)
		drupalOrgService := NewMockDrupalOrg(t)
		worktree := NewMockWorktree(t)

		composerService.EXPECT().GetConfig(mock.Anything, path, "extra.patches").
			Return(`{"drupal/core":{"Issue #1: [t](u)":"`+depPatch+`"}}`, nil)
		composerService.EXPECT().Update(mock.Anything, path, []string{}, []string{}, false, true).
			Return([]composer.PackageChange{}, nil)
		composerService.EXPECT().IsPackageInstalled(mock.Anything, path, "drupal/core").Return(true, nil)
		composerService.EXPECT().GetDependencyPatches(mock.Anything, path).
			Return(map[string]map[string]bool{"drupal/core": {depPatch: true}}, nil)

		var written string
		composerService.EXPECT().SetConfig(mock.Anything, path, "extra.patches", mock.Anything).
			Run(func(_ context.Context, _, _, value string) { written = value }).Return(nil)
		composerService.EXPECT().UpdateLockHash(mock.Anything, path).Return(nil)
		worktree.EXPECT().AddGlob("composer.*").Return(nil)
		worktree.EXPECT().Commit("Update patches", mock.Anything).Return(plumbing.NewHash(""), nil)

		h := &ComposerPatches1{logger: logger, composer: composerService, drupalOrg: drupalOrgService}
		e := services.NewPreComposerUpdateEvent(t.Context(), path, worktree, []string{}, []string{}, false)

		assert.NoError(t, h.preComposerUpdateHandler(e))
		assert.JSONEq(t, `{}`, written)
		assert.True(t, h.patchUpdates.Changes())
	})

	t.Run("appends conflicting package to PackagesToKeep", func(t *testing.T) {
		composerService := NewMockComposer(t)
		drupalOrgService := NewMockDrupalOrg(t)
		worktree := NewMockWorktree(t)

		composerService.EXPECT().GetConfig(mock.Anything, path, "extra.patches").
			Return(`{"drupal/core":{"local patch":"patches/x.patch"}}`, nil)
		composerService.EXPECT().Update(mock.Anything, path, []string{}, []string{}, false, true).
			Return([]composer.PackageChange{{Action: "Upgrade", Package: "drupal/core", From: "8.7.0", To: "8.8.0"}}, nil)
		composerService.EXPECT().IsPackageInstalled(mock.Anything, path, "drupal/core").Return(true, nil)
		composerService.EXPECT().GetDependencyPatches(mock.Anything, path).Return(nil, nil)
		drupalOrgService.EXPECT().FindIssueNumber("local patch").Return("", false)
		drupalOrgService.EXPECT().FindIssueNumber("patches/x.patch").Return("", false)
		composerService.EXPECT().CheckIfPatchApplies(mock.Anything, "drupal/core", "8.8.0", path+"/patches/x.patch").Return(false, nil)

		composerService.EXPECT().SetConfig(mock.Anything, path, "extra.patches", mock.Anything).Return(nil)
		composerService.EXPECT().UpdateLockHash(mock.Anything, path).Return(nil)
		worktree.EXPECT().AddGlob("composer.*").Return(nil)
		worktree.EXPECT().Commit("Update patches", mock.Anything).Return(plumbing.NewHash(""), nil)

		h := &ComposerPatches1{logger: logger, composer: composerService, drupalOrg: drupalOrgService}
		e := services.NewPreComposerUpdateEvent(t.Context(), path, worktree, []string{}, []string{}, false)

		assert.NoError(t, h.preComposerUpdateHandler(e))
		assert.Contains(t, e.PackagesToKeep, "drupal/core:8.7.0")
	})

	t.Run("does not persist when nothing changes", func(t *testing.T) {
		composerService := NewMockComposer(t)
		drupalOrgService := NewMockDrupalOrg(t)
		worktree := NewMockWorktree(t)

		composerService.EXPECT().GetConfig(mock.Anything, path, "extra.patches").
			Return(`{"drupal/core":{"local patch":"patches/x.patch"}}`, nil)
		composerService.EXPECT().Update(mock.Anything, path, []string{}, []string{}, false, true).
			Return([]composer.PackageChange{{Action: "Upgrade", Package: "drupal/core", To: "8.8.0"}}, nil)
		composerService.EXPECT().IsPackageInstalled(mock.Anything, path, "drupal/core").Return(true, nil)
		composerService.EXPECT().GetDependencyPatches(mock.Anything, path).Return(nil, nil)
		drupalOrgService.EXPECT().FindIssueNumber("local patch").Return("", false)
		drupalOrgService.EXPECT().FindIssueNumber("patches/x.patch").Return("", false)
		composerService.EXPECT().CheckIfPatchApplies(mock.Anything, "drupal/core", "8.8.0", path+"/patches/x.patch").Return(true, nil)

		h := &ComposerPatches1{logger: logger, composer: composerService, drupalOrg: drupalOrgService}
		e := services.NewPreComposerUpdateEvent(t.Context(), path, worktree, []string{}, []string{}, false)

		// No SetConfig/UpdateLockHash/Commit expectations: the mock fails if they are called.
		assert.NoError(t, h.preComposerUpdateHandler(e))
		assert.False(t, h.patchUpdates.Changes())
	})

	t.Run("treats missing extra.patches as empty", func(t *testing.T) {
		composerService := NewMockComposer(t)
		worktree := NewMockWorktree(t)

		composerService.EXPECT().GetConfig(mock.Anything, path, "extra.patches").Return("", errors.New("not defined"))
		composerService.EXPECT().Update(mock.Anything, path, []string{}, []string{}, false, true).
			Return([]composer.PackageChange{}, nil)
		composerService.EXPECT().GetDependencyPatches(mock.Anything, path).Return(nil, nil)

		h := &ComposerPatches1{logger: logger, composer: composerService}
		e := services.NewPreComposerUpdateEvent(t.Context(), path, worktree, []string{}, []string{}, false)

		assert.NoError(t, h.preComposerUpdateHandler(e))
	})

	t.Run("returns error on invalid patches JSON", func(t *testing.T) {
		composerService := NewMockComposer(t)
		worktree := NewMockWorktree(t)

		composerService.EXPECT().GetConfig(mock.Anything, path, "extra.patches").Return("not-json", nil)

		h := &ComposerPatches1{logger: logger, composer: composerService}
		e := services.NewPreComposerUpdateEvent(t.Context(), path, worktree, []string{}, []string{}, false)

		err := h.preComposerUpdateHandler(e)
		assert.ErrorContains(t, err, "failed to unmarshal patches")
	})

	t.Run("propagates persistence errors", func(t *testing.T) {
		const depPatch = "https://example.com/x.patch"

		// Each case fails one step of the persist sequence; later steps are not expected.
		cases := []struct {
			name    string
			arrange func(c *MockComposer, w *MockWorktree)
			wantErr string
		}{
			{
				name: "SetConfig",
				arrange: func(c *MockComposer, _ *MockWorktree) {
					c.EXPECT().SetConfig(mock.Anything, path, "extra.patches", mock.Anything).Return(errors.New("x"))
				},
				wantErr: "failed to set composer config",
			},
			{
				name: "UpdateLockHash",
				arrange: func(c *MockComposer, _ *MockWorktree) {
					c.EXPECT().SetConfig(mock.Anything, path, "extra.patches", mock.Anything).Return(nil)
					c.EXPECT().UpdateLockHash(mock.Anything, path).Return(errors.New("x"))
				},
				wantErr: "failed to update composer lock hash",
			},
			{
				name: "AddGlob",
				arrange: func(c *MockComposer, w *MockWorktree) {
					c.EXPECT().SetConfig(mock.Anything, path, "extra.patches", mock.Anything).Return(nil)
					c.EXPECT().UpdateLockHash(mock.Anything, path).Return(nil)
					w.EXPECT().AddGlob("composer.*").Return(errors.New("x"))
				},
				wantErr: "failed to add composer.* files",
			},
			{
				name: "Commit",
				arrange: func(c *MockComposer, w *MockWorktree) {
					c.EXPECT().SetConfig(mock.Anything, path, "extra.patches", mock.Anything).Return(nil)
					c.EXPECT().UpdateLockHash(mock.Anything, path).Return(nil)
					w.EXPECT().AddGlob("composer.*").Return(nil)
					w.EXPECT().Commit("Update patches", mock.Anything).Return(plumbing.NewHash(""), errors.New("x"))
				},
				wantErr: "failed to commit patches",
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				composerService := NewMockComposer(t)
				worktree := NewMockWorktree(t)

				composerService.EXPECT().GetConfig(mock.Anything, path, "extra.patches").
					Return(`{"drupal/core":{"Issue #1: [t](u)":"`+depPatch+`"}}`, nil)
				composerService.EXPECT().Update(mock.Anything, path, []string{}, []string{}, false, true).
					Return([]composer.PackageChange{}, nil)
				composerService.EXPECT().IsPackageInstalled(mock.Anything, path, "drupal/core").Return(true, nil)
				composerService.EXPECT().GetDependencyPatches(mock.Anything, path).
					Return(map[string]map[string]bool{"drupal/core": {depPatch: true}}, nil)
				tc.arrange(composerService, worktree)

				h := &ComposerPatches1{logger: logger, composer: composerService}
				e := services.NewPreComposerUpdateEvent(t.Context(), path, worktree, []string{}, []string{}, false)

				assert.ErrorContains(t, h.preComposerUpdateHandler(e), tc.wantErr)
			})
		}
	})

	t.Run("returns error when composer update fails", func(t *testing.T) {
		composerService := NewMockComposer(t)
		worktree := NewMockWorktree(t)

		composerService.EXPECT().GetConfig(mock.Anything, path, "extra.patches").Return("{}", nil)
		composerService.EXPECT().Update(mock.Anything, path, []string{}, []string{}, false, true).
			Return(nil, errors.New("boom"))

		h := &ComposerPatches1{logger: logger, composer: composerService}
		e := services.NewPreComposerUpdateEvent(t.Context(), path, worktree, []string{}, []string{}, false)

		err := h.preComposerUpdateHandler(e)
		assert.ErrorContains(t, err, "failed to get composer updates")
	})
}
