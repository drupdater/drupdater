package addon

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/drupdater/drupdater/internal/composer"
	"github.com/drupdater/drupdater/internal/workflow"
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

		composerService := NewMockPatchesComposer(t)
		drupalOrgService := NewMockDrupalOrg(t)
		committer := &fakeCommitter{}

		composerService.EXPECT().GetConfig(anyCtx, path, "extra.patches").
			Return(`{"drupal/core":{"Issue #1: [t](u)":"`+depPatch+`"}}`, nil)
		composerService.EXPECT().Update(anyCtx, path, []string(nil), []string(nil), false, true).
			Return([]composer.PackageChange{}, nil)
		composerService.EXPECT().IsPackageInstalled(anyCtx, path, "drupal/core").Return(true, nil)
		composerService.EXPECT().GetDependencyPatches(anyCtx, path).
			Return(map[string]map[string]bool{"drupal/core": {depPatch: true}}, nil)

		var written string
		composerService.EXPECT().SetConfig(anyCtx, path, "extra.patches", mock.Anything).
			Run(func(_ context.Context, _, _, value string) { written = value }).Return(nil)
		composerService.EXPECT().UpdateLockHash(anyCtx, path).Return(nil)

		h := &ComposerPatches1{logger: logger, composer: composerService, drupalOrg: drupalOrgService}
		e := workflow.NewPreComposerUpdateEvent(t.Context(), path, committer, workflow.UpdatePlan{})

		plan, err := h.preComposerUpdateHandler(e)
		require.NoError(t, err)
		assert.Zero(t, plan, "a removed patch pins nothing")
		assert.JSONEq(t, `{}`, written)
		assert.True(t, h.patchUpdates.Changes())
		assert.Equal(t, []recordedCommit{{message: "Update patches", paths: []string{"composer.json", "composer.lock"}}}, committer.commits,
			"a remote patch has no file to commit")
	})

	t.Run("proposes pinning a package whose patch conflicts", func(t *testing.T) {
		composerService := NewMockPatchesComposer(t)
		drupalOrgService := NewMockDrupalOrg(t)
		committer := &fakeCommitter{}

		composerService.EXPECT().GetConfig(anyCtx, path, "extra.patches").
			Return(`{"drupal/core":{"local patch":"patches/x.patch"}}`, nil)
		composerService.EXPECT().Update(anyCtx, path, []string(nil), []string(nil), false, true).
			Return([]composer.PackageChange{{Action: "Upgrade", Package: "drupal/core", From: "8.7.0", To: "8.8.0"}}, nil)
		composerService.EXPECT().IsPackageInstalled(anyCtx, path, "drupal/core").Return(true, nil)
		composerService.EXPECT().GetDependencyPatches(anyCtx, path).Return(nil, nil)
		drupalOrgService.EXPECT().FindIssueNumber("local patch").Return("", false)
		drupalOrgService.EXPECT().FindIssueNumber("patches/x.patch").Return("", false)
		composerService.EXPECT().CheckIfPatchApplies(anyCtx, mock.Anything, "drupal/core", "8.8.0", path+"/patches/x.patch").Return(false, nil)

		composerService.EXPECT().SetConfig(anyCtx, path, "extra.patches", mock.Anything).Return(nil)
		composerService.EXPECT().UpdateLockHash(anyCtx, path).Return(nil)

		h := &ComposerPatches1{logger: logger, composer: composerService, drupalOrg: drupalOrgService}
		e := workflow.NewPreComposerUpdateEvent(t.Context(), path, committer, workflow.UpdatePlan{})

		plan, err := h.preComposerUpdateHandler(e)
		require.NoError(t, err)
		assert.Equal(t, workflow.UpdatePlan{Keep: []workflow.Pin{{Package: "drupal/core", Version: "8.7.0"}}}, plan)
		assert.Equal(t, []recordedCommit{{message: "Update patches", paths: []string{"composer.json", "composer.lock"}}}, committer.commits,
			"a conflicting patch stays where it is")
	})

	t.Run("does not persist when nothing changes", func(t *testing.T) {
		composerService := NewMockPatchesComposer(t)
		drupalOrgService := NewMockDrupalOrg(t)
		committer := &fakeCommitter{}

		composerService.EXPECT().GetConfig(anyCtx, path, "extra.patches").
			Return(`{"drupal/core":{"local patch":"patches/x.patch"}}`, nil)
		composerService.EXPECT().Update(anyCtx, path, []string(nil), []string(nil), false, true).
			Return([]composer.PackageChange{{Action: "Upgrade", Package: "drupal/core", To: "8.8.0"}}, nil)
		composerService.EXPECT().IsPackageInstalled(anyCtx, path, "drupal/core").Return(true, nil)
		composerService.EXPECT().GetDependencyPatches(anyCtx, path).Return(nil, nil)
		drupalOrgService.EXPECT().FindIssueNumber("local patch").Return("", false)
		drupalOrgService.EXPECT().FindIssueNumber("patches/x.patch").Return("", false)
		composerService.EXPECT().CheckIfPatchApplies(anyCtx, mock.Anything, "drupal/core", "8.8.0", path+"/patches/x.patch").Return(true, nil)

		h := &ComposerPatches1{logger: logger, composer: composerService, drupalOrg: drupalOrgService}
		e := workflow.NewPreComposerUpdateEvent(t.Context(), path, committer, workflow.UpdatePlan{})

		// No SetConfig/UpdateLockHash expectations: the mock fails if they are called.
		plan, err := h.preComposerUpdateHandler(e)
		require.NoError(t, err)
		assert.Zero(t, plan)
		assert.False(t, h.patchUpdates.Changes())
		assert.Empty(t, committer.commits)
	})

	t.Run("treats missing extra.patches as empty", func(t *testing.T) {
		composerService := NewMockPatchesComposer(t)
		committer := &fakeCommitter{}

		composerService.EXPECT().GetConfig(anyCtx, path, "extra.patches").Return("", errors.New("not defined"))
		composerService.EXPECT().Update(anyCtx, path, []string(nil), []string(nil), false, true).
			Return([]composer.PackageChange{}, nil)
		composerService.EXPECT().GetDependencyPatches(anyCtx, path).Return(nil, nil)

		h := &ComposerPatches1{logger: logger, composer: composerService}
		e := workflow.NewPreComposerUpdateEvent(t.Context(), path, committer, workflow.UpdatePlan{})

		_, err := h.preComposerUpdateHandler(e)
		require.NoError(t, err)
	})

	t.Run("returns error on invalid patches JSON", func(t *testing.T) {
		composerService := NewMockPatchesComposer(t)
		committer := &fakeCommitter{}

		composerService.EXPECT().GetConfig(anyCtx, path, "extra.patches").Return("not-json", nil)

		h := &ComposerPatches1{logger: logger, composer: composerService}
		e := workflow.NewPreComposerUpdateEvent(t.Context(), path, committer, workflow.UpdatePlan{})

		_, err := h.preComposerUpdateHandler(e)
		var syntaxErr *json.SyntaxError
		require.ErrorAs(t, err, &syntaxErr)
		require.ErrorContains(t, err, "failed to unmarshal patches")
	})

	t.Run("propagates persistence errors", func(t *testing.T) {
		const depPatch = "https://example.com/x.patch"

		// Each case fails one step of the persist sequence; later steps are not expected.
		cases := []struct {
			name    string
			arrange func(c *MockPatchesComposer, cm *fakeCommitter)
			wantErr string
		}{
			{
				name: "SetConfig",
				arrange: func(c *MockPatchesComposer, _ *fakeCommitter) {
					c.EXPECT().SetConfig(anyCtx, path, "extra.patches", mock.Anything).Return(assert.AnError)
				},
				wantErr: "failed to set composer config",
			},
			{
				name: "UpdateLockHash",
				arrange: func(c *MockPatchesComposer, _ *fakeCommitter) {
					c.EXPECT().SetConfig(anyCtx, path, "extra.patches", mock.Anything).Return(nil)
					c.EXPECT().UpdateLockHash(anyCtx, path).Return(assert.AnError)
				},
				wantErr: "failed to update composer lock hash",
			},
			{
				name: "Commit",
				arrange: func(c *MockPatchesComposer, cm *fakeCommitter) {
					c.EXPECT().SetConfig(anyCtx, path, "extra.patches", mock.Anything).Return(nil)
					c.EXPECT().UpdateLockHash(anyCtx, path).Return(nil)
					cm.err = assert.AnError
				},
				wantErr: "failed to commit patches",
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				composerService := NewMockPatchesComposer(t)
				committer := &fakeCommitter{}

				composerService.EXPECT().GetConfig(anyCtx, path, "extra.patches").
					Return(`{"drupal/core":{"Issue #1: [t](u)":"`+depPatch+`"}}`, nil)
				composerService.EXPECT().Update(anyCtx, path, []string(nil), []string(nil), false, true).
					Return([]composer.PackageChange{}, nil)
				composerService.EXPECT().IsPackageInstalled(anyCtx, path, "drupal/core").Return(true, nil)
				composerService.EXPECT().GetDependencyPatches(anyCtx, path).
					Return(map[string]map[string]bool{"drupal/core": {depPatch: true}}, nil)
				tc.arrange(composerService, committer)

				h := &ComposerPatches1{logger: logger, composer: composerService}
				e := workflow.NewPreComposerUpdateEvent(t.Context(), path, committer, workflow.UpdatePlan{})

				_, err := h.preComposerUpdateHandler(e)
				require.ErrorIs(t, err, assert.AnError)
				require.ErrorContains(t, err, tc.wantErr)
			})
		}
	})

	t.Run("returns error when composer update fails", func(t *testing.T) {
		composerService := NewMockPatchesComposer(t)
		committer := &fakeCommitter{}

		composerService.EXPECT().GetConfig(anyCtx, path, "extra.patches").Return("{}", nil)
		composerService.EXPECT().Update(anyCtx, path, []string(nil), []string(nil), false, true).
			Return(nil, assert.AnError)

		h := &ComposerPatches1{logger: logger, composer: composerService}
		e := workflow.NewPreComposerUpdateEvent(t.Context(), path, committer, workflow.UpdatePlan{})

		_, err := h.preComposerUpdateHandler(e)
		require.ErrorIs(t, err, assert.AnError)
		require.ErrorContains(t, err, "failed to get composer updates")
	})

	t.Run("dry-runs the update as earlier stages planned it", func(t *testing.T) {
		composerService := NewMockPatchesComposer(t)

		composerService.EXPECT().GetConfig(anyCtx, path, "extra.patches").Return("{}", nil)
		composerService.EXPECT().Update(anyCtx, path, []string{"drupal/core"}, []string{"drupal/token:1.13.0"}, true, true).
			Return(nil, nil)
		composerService.EXPECT().GetDependencyPatches(anyCtx, path).Return(nil, nil)

		h := &ComposerPatches1{logger: logger, composer: composerService}
		e := workflow.NewPreComposerUpdateEvent(t.Context(), path, &fakeCommitter{}, workflow.UpdatePlan{
			Only:           []string{"drupal/core"},
			Keep:           []workflow.Pin{{Package: "drupal/token", Version: "1.13.0"}},
			MinimalChanges: true,
		})

		plan, err := h.preComposerUpdateHandler(e)
		require.NoError(t, err)
		assert.Zero(t, plan, "the settled plan is not proposed again")
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
