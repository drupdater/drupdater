package addon

import (
	"context"
	"errors"
	"testing"

	"github.com/drupdater/drupdater/internal/workflow"
	"github.com/drupdater/drupdater/pkg/composer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestPreComposerUpdateHandler(t *testing.T) {
	logger := zap.NewNop()
	path := "/repo"

	t.Run("persists and commits when a patch is removed", func(t *testing.T) {
		const depPatch = "https://www.drupal.org/files/issues/x.patch"

		composerService := NewMockComposer(t)
		drupalOrgService := NewMockDrupalOrg(t)
		committer := &fakeCommitter{}

		composerService.EXPECT().GetConfig(anyCtx, path, "extra.patches").
			Return(`{"drupal/core":{"Issue #1: [t](u)":"`+depPatch+`"}}`, nil)
		composerService.EXPECT().Update(anyCtx, path, []string{}, []string{}, false, true).
			Return([]composer.PackageChange{}, nil)
		composerService.EXPECT().IsPackageInstalled(anyCtx, path, "drupal/core").Return(true, nil)
		composerService.EXPECT().GetDependencyPatches(anyCtx, path).
			Return(map[string]map[string]bool{"drupal/core": {depPatch: true}}, nil)

		var written string
		composerService.EXPECT().SetConfig(anyCtx, path, "extra.patches", mock.Anything).
			Run(func(_ context.Context, _, _, value string) { written = value }).Return(nil)
		composerService.EXPECT().UpdateLockHash(anyCtx, path).Return(nil)

		h := &ComposerPatches1{logger: logger, composer: composerService, drupalOrg: drupalOrgService}
		e := workflow.NewPreComposerUpdateEvent(t.Context(), path, committer, []string{}, []string{}, false)

		require.NoError(t, h.preComposerUpdateHandler(e))
		assert.JSONEq(t, `{}`, written)
		assert.True(t, h.patchUpdates.Changes())
		assert.Equal(t, []recordedCommit{{message: "Update patches", paths: []string{"composer.json", "composer.lock"}}}, committer.commits,
			"a remote patch has no file to commit")
	})

	t.Run("appends conflicting package to PackagesToKeep", func(t *testing.T) {
		composerService := NewMockComposer(t)
		drupalOrgService := NewMockDrupalOrg(t)
		committer := &fakeCommitter{}

		composerService.EXPECT().GetConfig(anyCtx, path, "extra.patches").
			Return(`{"drupal/core":{"local patch":"patches/x.patch"}}`, nil)
		composerService.EXPECT().Update(anyCtx, path, []string{}, []string{}, false, true).
			Return([]composer.PackageChange{{Action: "Upgrade", Package: "drupal/core", From: "8.7.0", To: "8.8.0"}}, nil)
		composerService.EXPECT().IsPackageInstalled(anyCtx, path, "drupal/core").Return(true, nil)
		composerService.EXPECT().GetDependencyPatches(anyCtx, path).Return(nil, nil)
		drupalOrgService.EXPECT().FindIssueNumber("local patch").Return("", false)
		drupalOrgService.EXPECT().FindIssueNumber("patches/x.patch").Return("", false)
		composerService.EXPECT().CheckIfPatchApplies(anyCtx, mock.Anything, "drupal/core", "8.8.0", path+"/patches/x.patch").Return(false, nil)

		composerService.EXPECT().SetConfig(anyCtx, path, "extra.patches", mock.Anything).Return(nil)
		composerService.EXPECT().UpdateLockHash(anyCtx, path).Return(nil)

		h := &ComposerPatches1{logger: logger, composer: composerService, drupalOrg: drupalOrgService}
		e := workflow.NewPreComposerUpdateEvent(t.Context(), path, committer, []string{}, []string{}, false)

		require.NoError(t, h.preComposerUpdateHandler(e))
		assert.Contains(t, e.PackagesToKeep, "drupal/core:8.7.0")
		assert.Equal(t, []recordedCommit{{message: "Update patches", paths: []string{"composer.json", "composer.lock"}}}, committer.commits,
			"a conflicting patch stays where it is")
	})

	t.Run("does not persist when nothing changes", func(t *testing.T) {
		composerService := NewMockComposer(t)
		drupalOrgService := NewMockDrupalOrg(t)
		committer := &fakeCommitter{}

		composerService.EXPECT().GetConfig(anyCtx, path, "extra.patches").
			Return(`{"drupal/core":{"local patch":"patches/x.patch"}}`, nil)
		composerService.EXPECT().Update(anyCtx, path, []string{}, []string{}, false, true).
			Return([]composer.PackageChange{{Action: "Upgrade", Package: "drupal/core", To: "8.8.0"}}, nil)
		composerService.EXPECT().IsPackageInstalled(anyCtx, path, "drupal/core").Return(true, nil)
		composerService.EXPECT().GetDependencyPatches(anyCtx, path).Return(nil, nil)
		drupalOrgService.EXPECT().FindIssueNumber("local patch").Return("", false)
		drupalOrgService.EXPECT().FindIssueNumber("patches/x.patch").Return("", false)
		composerService.EXPECT().CheckIfPatchApplies(anyCtx, mock.Anything, "drupal/core", "8.8.0", path+"/patches/x.patch").Return(true, nil)

		h := &ComposerPatches1{logger: logger, composer: composerService, drupalOrg: drupalOrgService}
		e := workflow.NewPreComposerUpdateEvent(t.Context(), path, committer, []string{}, []string{}, false)

		// No SetConfig/UpdateLockHash expectations: the mock fails if they are called.
		require.NoError(t, h.preComposerUpdateHandler(e))
		assert.False(t, h.patchUpdates.Changes())
		assert.Empty(t, committer.commits)
	})

	t.Run("treats missing extra.patches as empty", func(t *testing.T) {
		composerService := NewMockComposer(t)
		committer := &fakeCommitter{}

		composerService.EXPECT().GetConfig(anyCtx, path, "extra.patches").Return("", errors.New("not defined"))
		composerService.EXPECT().Update(anyCtx, path, []string{}, []string{}, false, true).
			Return([]composer.PackageChange{}, nil)
		composerService.EXPECT().GetDependencyPatches(anyCtx, path).Return(nil, nil)

		h := &ComposerPatches1{logger: logger, composer: composerService}
		e := workflow.NewPreComposerUpdateEvent(t.Context(), path, committer, []string{}, []string{}, false)

		require.NoError(t, h.preComposerUpdateHandler(e))
	})

	t.Run("returns error on invalid patches JSON", func(t *testing.T) {
		composerService := NewMockComposer(t)
		committer := &fakeCommitter{}

		composerService.EXPECT().GetConfig(anyCtx, path, "extra.patches").Return("not-json", nil)

		h := &ComposerPatches1{logger: logger, composer: composerService}
		e := workflow.NewPreComposerUpdateEvent(t.Context(), path, committer, []string{}, []string{}, false)

		err := h.preComposerUpdateHandler(e)
		require.ErrorContains(t, err, "failed to unmarshal patches")
	})

	t.Run("propagates persistence errors", func(t *testing.T) {
		const depPatch = "https://example.com/x.patch"

		// Each case fails one step of the persist sequence; later steps are not expected.
		cases := []struct {
			name    string
			arrange func(c *MockComposer, cm *fakeCommitter)
			wantErr string
		}{
			{
				name: "SetConfig",
				arrange: func(c *MockComposer, _ *fakeCommitter) {
					c.EXPECT().SetConfig(anyCtx, path, "extra.patches", mock.Anything).Return(errors.New("x"))
				},
				wantErr: "failed to set composer config",
			},
			{
				name: "UpdateLockHash",
				arrange: func(c *MockComposer, _ *fakeCommitter) {
					c.EXPECT().SetConfig(anyCtx, path, "extra.patches", mock.Anything).Return(nil)
					c.EXPECT().UpdateLockHash(anyCtx, path).Return(errors.New("x"))
				},
				wantErr: "failed to update composer lock hash",
			},
			{
				name: "Commit",
				arrange: func(c *MockComposer, cm *fakeCommitter) {
					c.EXPECT().SetConfig(anyCtx, path, "extra.patches", mock.Anything).Return(nil)
					c.EXPECT().UpdateLockHash(anyCtx, path).Return(nil)
					cm.err = errors.New("x")
				},
				wantErr: "failed to commit patches",
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				composerService := NewMockComposer(t)
				committer := &fakeCommitter{}

				composerService.EXPECT().GetConfig(anyCtx, path, "extra.patches").
					Return(`{"drupal/core":{"Issue #1: [t](u)":"`+depPatch+`"}}`, nil)
				composerService.EXPECT().Update(anyCtx, path, []string{}, []string{}, false, true).
					Return([]composer.PackageChange{}, nil)
				composerService.EXPECT().IsPackageInstalled(anyCtx, path, "drupal/core").Return(true, nil)
				composerService.EXPECT().GetDependencyPatches(anyCtx, path).
					Return(map[string]map[string]bool{"drupal/core": {depPatch: true}}, nil)
				tc.arrange(composerService, committer)

				h := &ComposerPatches1{logger: logger, composer: composerService}
				e := workflow.NewPreComposerUpdateEvent(t.Context(), path, committer, []string{}, []string{}, false)

				require.ErrorContains(t, h.preComposerUpdateHandler(e), tc.wantErr)
			})
		}
	})

	t.Run("returns error when composer update fails", func(t *testing.T) {
		composerService := NewMockComposer(t)
		committer := &fakeCommitter{}

		composerService.EXPECT().GetConfig(anyCtx, path, "extra.patches").Return("{}", nil)
		composerService.EXPECT().Update(anyCtx, path, []string{}, []string{}, false, true).
			Return(nil, errors.New("boom"))

		h := &ComposerPatches1{logger: logger, composer: composerService}
		e := workflow.NewPreComposerUpdateEvent(t.Context(), path, committer, []string{}, []string{}, false)

		err := h.preComposerUpdateHandler(e)
		require.ErrorContains(t, err, "failed to get composer updates")
	})
}

func TestPatchUpdatesChangedPaths(t *testing.T) {
	updates := PatchUpdates{
		Removed: []RemovedPatch{
			{PatchPath: "patches/fixed.patch"},
			{PatchPath: "https://www.drupal.org/files/issues/remote.patch"},
		},
		Updated:   []UpdatedPatch{{PreviousPatchPath: "patches/old.diff", NewPatchPath: "patches/drupal/new.diff"}},
		Conflicts: []ConflictPatch{{PatchPath: "patches/kept.patch"}},
	}

	assert.Equal(t,
		[]string{"composer.json", "composer.lock", "patches/fixed.patch", "patches/old.diff", "patches/drupal/new.diff"},
		updates.changedPaths("composer.json", "composer.lock"),
		"every local file the update removed or added; a URL is not a file and a conflict changes none")
}
