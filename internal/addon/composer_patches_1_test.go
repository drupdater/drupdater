package addon

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/drupdater/drupdater/internal/composer"
	"github.com/drupdater/drupdater/internal/drupalorg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestIsRemotePatch(t *testing.T) {
	tests := []struct {
		name  string
		patch string
		want  bool
	}{
		{name: "https URL", patch: "https://www.drupal.org/files/x.patch", want: true},
		{name: "http URL", patch: "http://example.com/x.patch", want: true},
		{name: "relative local path", patch: "patches/core/x.patch", want: false},
		{name: "absolute local path is not remote", patch: "/patches/core/x.patch", want: false},
		{name: "empty", patch: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isRemotePatch(tt.patch))
		})
	}
}

func TestCleanURLString(t *testing.T) {
	h := &ComposerPatches1{}
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "spaces become underscores and lower-cased", in: "Alot of Problems", want: "alot_of_problems"},
		{name: "path separators and reserved chars are stripped", in: "fix a/b: [x]?#", want: "fix_ab_x"},
		{name: "keeps dots, dashes and underscores", in: "v1.2-beta_final", want: "v1.2-beta_final"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := h.cleanURLString(tt.in)
			assert.Equal(t, tt.want, got)
			assert.NotContains(t, got, "/") // never yields a path separator
		})
	}
}

func TestUpdatePatches(t *testing.T) {

	logger := zap.NewNop()

	t.Run("Local patch still applies", func(t *testing.T) {

		composerService := NewMockPatchesComposer(t)
		drupalOrgService := NewMockDrupalOrg(t)

		var removed []string

		drupalOrgService.EXPECT().FindIssueNumber("local patch without issue number").Return("", false)
		drupalOrgService.EXPECT().FindIssueNumber("patches/core/0001-local-patch.patch").Return("", false)

		composerService.EXPECT().CheckIfPatchApplies(anyCtx, mock.Anything, "drupal/core", "8.8.0", "/tmp/patches/core/0001-local-patch.patch").Return(true, nil)
		updater := &ComposerPatches1{
			logger:    logger,
			composer:  composerService,
			drupalOrg: drupalOrgService,
		}

		operations := []composer.PackageChange{
			{
				Action:  "Upgrade",
				Package: "drupal/core",
				To:      "8.8.0",
			},
		}
		patches := map[string]map[string]string{
			"drupal/core": {
				"local patch without issue number": "patches/core/0001-local-patch.patch",
			},
		}

		updater.removeFile = recordRemovals(&removed)
		report, declared := updater.updatePatches(t.Context(), "/tmp", installedProject(patches), operations, declaredFrom(patches))
		newPatches := asMap(declared)
		assert.Empty(t, removed)
		assert.Equal(t, patches, newPatches)
		assert.False(t, report.Changes())

		composerService.AssertExpectations(t)
		drupalOrgService.AssertExpectations(t)
	})

	t.Run("Local patch is not deduplicated against dependencies", func(t *testing.T) {
		// A local (relative) path that happens to match a dependency string must NOT be
		// removed: local paths are package-relative, so they are not the same file.
		composerService := NewMockPatchesComposer(t)
		drupalOrgService := NewMockDrupalOrg(t)
		var removed []string
		drupalOrgService.EXPECT().FindIssueNumber(mock.Anything).Return("", false)
		composerService.EXPECT().CheckIfPatchApplies(anyCtx, mock.Anything, "drupal/core", "8.8.0", "/tmp/patches/local.patch").Return(true, nil)

		updater := &ComposerPatches1{logger: logger, composer: composerService, drupalOrg: drupalOrgService}
		operations := []composer.PackageChange{{Action: "Upgrade", Package: "drupal/core", To: "8.8.0"}}
		patches := map[string]map[string]string{"drupal/core": {"local": "patches/local.patch"}}
		project := installedProject(patches)
		project.DependencyPatches = map[string][]string{"drupal/core": {"patches/local.patch"}}

		updater.removeFile = recordRemovals(&removed)
		report, declared := updater.updatePatches(t.Context(), "/tmp", project, operations, declaredFrom(patches))
		newPatches := asMap(declared)
		assert.Empty(t, removed)
		assert.Equal(t, "patches/local.patch", newPatches["drupal/core"]["local"])
		assert.Empty(t, report.Removed)
	})

	t.Run("Multiple patches apply together", func(t *testing.T) {
		composerService := NewMockPatchesComposer(t)
		drupalOrgService := NewMockDrupalOrg(t)
		var removed []string
		drupalOrgService.EXPECT().FindIssueNumber(mock.Anything).Return("", false)
		composerService.EXPECT().CheckIfPatchApplies(anyCtx, mock.Anything, "drupal/core", "8.8.0", "/tmp/patches/a.patch").Return(true, nil)
		composerService.EXPECT().CheckIfPatchApplies(anyCtx, mock.Anything, "drupal/core", "8.8.0", "/tmp/patches/b.patch").Return(true, nil)
		composerService.EXPECT().CheckIfPatchesApply(anyCtx, mock.Anything, "drupal/core", "8.8.0", mock.Anything).Return(true, nil)

		updater := &ComposerPatches1{logger: logger, composer: composerService, drupalOrg: drupalOrgService}
		operations := []composer.PackageChange{{Action: "Upgrade", Package: "drupal/core", To: "8.8.0"}}
		patches := map[string]map[string]string{"drupal/core": {"a": "patches/a.patch", "b": "patches/b.patch"}}

		updater.removeFile = recordRemovals(&removed)
		report, _ := updater.updatePatches(t.Context(), "/tmp", installedProject(patches), operations, declaredFrom(patches))
		assert.Empty(t, removed)
		assert.False(t, report.Changes())
	})

	t.Run("Combined patch check error is non-fatal", func(t *testing.T) {
		composerService := NewMockPatchesComposer(t)
		drupalOrgService := NewMockDrupalOrg(t)
		var removed []string
		drupalOrgService.EXPECT().FindIssueNumber(mock.Anything).Return("", false)
		composerService.EXPECT().CheckIfPatchApplies(anyCtx, mock.Anything, "drupal/core", "8.8.0", "/tmp/patches/a.patch").Return(true, nil)
		composerService.EXPECT().CheckIfPatchApplies(anyCtx, mock.Anything, "drupal/core", "8.8.0", "/tmp/patches/b.patch").Return(true, nil)
		composerService.EXPECT().CheckIfPatchesApply(anyCtx, mock.Anything, "drupal/core", "8.8.0", mock.Anything).Return(false, assert.AnError)

		updater := &ComposerPatches1{logger: logger, composer: composerService, drupalOrg: drupalOrgService}
		operations := []composer.PackageChange{{Action: "Upgrade", Package: "drupal/core", To: "8.8.0"}}
		patches := map[string]map[string]string{"drupal/core": {"a": "patches/a.patch", "b": "patches/b.patch"}}

		updater.removeFile = recordRemovals(&removed)
		report, _ := updater.updatePatches(t.Context(), "/tmp", installedProject(patches), operations, declaredFrom(patches))
		assert.Empty(t, removed)
		assert.Empty(t, report.Conflicts)
	})

	t.Run("Patch already provided by a dependency is removed", func(t *testing.T) {
		composerService := NewMockPatchesComposer(t)
		drupalOrgService := NewMockDrupalOrg(t)
		var removed []string

		const depPatch = "https://www.drupal.org/files/issues/2024-07-16/2869592-disabled-update-module-71.patch"

		updater := &ComposerPatches1{
			logger:    logger,
			composer:  composerService,
			drupalOrg: drupalOrgService,
		}

		operations := []composer.PackageChange{
			{Action: "Upgrade", Package: "drupal/core", From: "10.5.0", To: "10.6.0"},
		}
		patches := map[string]map[string]string{
			"drupal/core": {
				"Issue #2869592: [Disabled update module](https://www.drupal.org/node/2869592)": depPatch,
			},
		}
		project := installedProject(patches)
		project.DependencyPatches = map[string][]string{"drupal/core": {depPatch}}

		updater.removeFile = recordRemovals(&removed)
		report, declared := updater.updatePatches(t.Context(), "/tmp", project, operations, declaredFrom(patches))
		newPatches := asMap(declared)
		assert.Empty(t, removed)
		assert.Empty(t, newPatches["drupal/core"], "patch provided by a dependency should be removed from root")
		// The whole record, not just the path: every field ends up in the merge request.
		require.Len(t, report.Removed, 1)
		assert.Equal(t, RemovedPatch{
			Package:          "drupal/core",
			PatchPath:        depPatch,
			PatchDescription: "Issue #2869592: [Disabled update module](https://www.drupal.org/node/2869592)",
			Reason:           "Patch is already applied by a dependency of drupal/core",
		}, report.Removed[0])
		assert.True(t, report.Changes())

		composerService.AssertExpectations(t)
		drupalOrgService.AssertExpectations(t)
	})

	t.Run("Local patch not applies", func(t *testing.T) {

		composerService := NewMockPatchesComposer(t)
		drupalOrgService := NewMockDrupalOrg(t)

		var removed []string

		drupalOrgService.EXPECT().FindIssueNumber("local patch without issue number").Return("", false)
		drupalOrgService.EXPECT().FindIssueNumber("patches/core/0001-local-patch.patch").Return("", false)

		composerService.EXPECT().CheckIfPatchApplies(anyCtx, mock.Anything, "drupal/core", "8.8.0", "/tmp/patches/core/0001-local-patch.patch").Return(false, nil)
		updater := &ComposerPatches1{
			logger:    logger,
			composer:  composerService,
			drupalOrg: drupalOrgService,
		}

		operations := []composer.PackageChange{
			{
				Action:  "Upgrade",
				Package: "drupal/core",
				From:    "8.7.0",
				To:      "8.8.0",
			},
		}
		patches := map[string]map[string]string{
			"drupal/core": {
				"local patch without issue number": "patches/core/0001-local-patch.patch",
			},
		}

		updater.removeFile = recordRemovals(&removed)
		report, declared := updater.updatePatches(t.Context(), "/tmp", installedProject(patches), operations, declaredFrom(patches))
		newPatches := asMap(declared)
		assert.Empty(t, removed)
		assert.Equal(t, map[string]map[string]string{
			"drupal/core": {
				"local patch without issue number": "patches/core/0001-local-patch.patch",
			},
		}, newPatches)
		assert.Equal(t, PatchUpdates{
			Conflicts: []ConflictPatch{
				{
					Package:          "drupal/core",
					PatchPath:        "patches/core/0001-local-patch.patch",
					FixedVersion:     "8.7.0",
					NewVersion:       "8.8.0",
					PatchDescription: "local patch without issue number",
				},
			},
		}, report)
		assert.True(t, report.Changes())

		composerService.AssertExpectations(t)
		drupalOrgService.AssertExpectations(t)
	})

	t.Run("Remote patch still applies", func(t *testing.T) {

		composerService := NewMockPatchesComposer(t)
		drupalOrgService := NewMockDrupalOrg(t)

		var removed []string

		drupalOrgService.EXPECT().FindIssueNumber("Issue #123456 \"With problems\"").Return("123456", true)
		drupalOrgService.EXPECT().GetIssue(anyCtx, "123456").Return(&drupalorg.Issue{
			ID:     "123456",
			Title:  "Alot of problems",
			Status: "1",
			URL:    "https://www.drupal.org/node/123456",
		}, nil)

		composerService.EXPECT().CheckIfPatchApplies(anyCtx, mock.Anything, "drupal/core", "8.8.0", "/tmp/patches/remote/0001-remote.patch").Return(true, nil)

		updater := &ComposerPatches1{
			logger:    logger,
			composer:  composerService,
			drupalOrg: drupalOrgService,
		}

		operations := []composer.PackageChange{
			{
				Action:  "Upgrade",
				Package: "drupal/core",
				From:    "8.7.0",
				To:      "8.8.0",
			},
		}
		patches := map[string]map[string]string{
			"drupal/core": {
				"Issue #123456 \"With problems\"": "patches/remote/0001-remote.patch",
			},
		}

		updater.removeFile = recordRemovals(&removed)
		report, declared := updater.updatePatches(t.Context(), "/tmp", installedProject(patches), operations, declaredFrom(patches))
		newPatches := asMap(declared)
		assert.Empty(t, removed)
		assert.Equal(t, map[string]map[string]string{
			"drupal/core": {
				"Issue #123456: [Alot of problems](https://www.drupal.org/node/123456)": "patches/remote/0001-remote.patch",
			},
		}, newPatches)
		assert.False(t, report.Changes())

		composerService.AssertExpectations(t)
		drupalOrgService.AssertExpectations(t)
	})

	t.Run("Current patch fails, remote patch still applies", func(t *testing.T) {

		composerService := NewMockPatchesComposer(t)
		drupalOrgService := NewMockDrupalOrg(t)

		var removed []string

		drupalOrgService.EXPECT().FindIssueNumber("Issue #123456 \"With problems\"").Return("123456", true)
		drupalOrgService.EXPECT().GetIssue(anyCtx, "123456").Return(&drupalorg.Issue{
			ID:     "123456",
			Title:  "Alot of problems",
			Status: "1",
			URL:    "https://www.drupal.org/node/123456",
			Project: struct {
				MaschineName string `json:"machine_name"`
			}{
				MaschineName: "drupal",
			},
		}, nil)

		composerService.EXPECT().CheckIfPatchApplies(anyCtx, mock.Anything, "drupal/core", "8.8.0", "/tmp/patches/remote/0001-remote.patch").Return(false, nil)
		composerService.EXPECT().CheckIfPatchApplies(anyCtx, mock.Anything, "drupal/core", "8.8.0", "/tmp/patches/drupal/123456-111111-alot_of_problems.diff").Return(true, nil)

		mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/project/drupal/-/merge_requests/1.diff", r.URL.Path)
			_, _ = w.Write([]byte("patch content"))
		}))
		defer mockServer.Close()

		drupalCode := NewMockDrupalCode(t)
		drupalCode.EXPECT().IssueForkMergeRequests(anyCtx, "drupal", "123456").Return([]drupalorg.ForkMergeRequest{
			{SHA: "111111", WebURL: mockServer.URL + "/project/drupal/-/merge_requests/1"},
		}, nil)

		updater := &ComposerPatches1{
			logger:     logger,
			composer:   composerService,
			drupalOrg:  drupalOrgService,
			drupalCode: drupalCode,
			httpClient: mockServer.Client(),
		}

		operations := []composer.PackageChange{
			{
				Action:  "Upgrade",
				Package: "drupal/core",
				From:    "8.7.0",
				To:      "8.8.0",
			},
		}
		patches := map[string]map[string]string{
			"drupal/core": {
				"Issue #123456 \"With problems\"": "patches/remote/0001-remote.patch",
			},
		}

		updater.removeFile = recordRemovals(&removed)
		report, declared := updater.updatePatches(t.Context(), "/tmp", installedProject(patches), operations, declaredFrom(patches))
		newPatches := asMap(declared)
		assert.Equal(t, []string{"/tmp/patches/remote/0001-remote.patch"}, removed)
		assert.Equal(t, map[string]map[string]string{
			"drupal/core": {
				"Issue #123456: [Alot of problems](https://www.drupal.org/node/123456)": "patches/drupal/123456-111111-alot_of_problems.diff",
			},
		}, newPatches)
		assert.True(t, report.Changes())

		composerService.AssertExpectations(t)
		drupalOrgService.AssertExpectations(t)
	})

	t.Run("Current patch fails, remote patch also fails", func(t *testing.T) {

		composerService := NewMockPatchesComposer(t)
		drupalOrgService := NewMockDrupalOrg(t)

		var removed []string

		drupalOrgService.EXPECT().FindIssueNumber("Issue #123456 \"With problems\"").Return("123456", true)
		drupalOrgService.EXPECT().GetIssue(anyCtx, "123456").Return(&drupalorg.Issue{
			ID:     "123456",
			Title:  "Alot of problems",
			Status: "1",
			URL:    "https://www.drupal.org/node/123456",
			Project: struct {
				MaschineName string `json:"machine_name"`
			}{
				MaschineName: "drupal",
			},
		}, nil)

		composerService.EXPECT().CheckIfPatchApplies(anyCtx, mock.Anything, "drupal/core", "8.8.0", "/tmp/patches/remote/0001-remote.patch").Return(false, nil)
		composerService.EXPECT().CheckIfPatchApplies(anyCtx, mock.Anything, "drupal/core", "8.8.0", "/tmp/patches/drupal/123456-111111-alot_of_problems.diff").Return(false, nil)

		mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/project/drupal/-/merge_requests/1.diff", r.URL.Path)
			_, _ = w.Write([]byte("patch content"))
		}))
		defer mockServer.Close()

		drupalCode := NewMockDrupalCode(t)
		drupalCode.EXPECT().IssueForkMergeRequests(anyCtx, "drupal", "123456").Return([]drupalorg.ForkMergeRequest{
			{SHA: "111111", WebURL: mockServer.URL + "/project/drupal/-/merge_requests/1"},
		}, nil)

		updater := &ComposerPatches1{
			logger:     logger,
			composer:   composerService,
			drupalOrg:  drupalOrgService,
			drupalCode: drupalCode,
			httpClient: mockServer.Client(),
		}

		operations := []composer.PackageChange{
			{
				Action:  "Upgrade",
				Package: "drupal/core",
				From:    "8.7.0",
				To:      "8.8.0",
			},
		}
		patches := map[string]map[string]string{
			"drupal/core": {
				"Issue #123456 \"With problems\"": "patches/remote/0001-remote.patch",
			},
		}

		updater.removeFile = recordRemovals(&removed)
		report, declared := updater.updatePatches(t.Context(), "/tmp", installedProject(patches), operations, declaredFrom(patches))
		newPatches := asMap(declared)
		assert.Empty(t, removed)
		assert.Equal(t, map[string]map[string]string{
			"drupal/core": {
				"Issue #123456: [Alot of problems](https://www.drupal.org/node/123456)": "patches/remote/0001-remote.patch",
			},
		}, newPatches)
		assert.Equal(t, PatchUpdates{
			Conflicts: []ConflictPatch{
				{
					Package:          "drupal/core",
					PatchPath:        "patches/remote/0001-remote.patch",
					FixedVersion:     "8.7.0",
					NewVersion:       "8.8.0",
					PatchDescription: "Issue #123456: [Alot of problems](https://www.drupal.org/node/123456)",
				},
			},
		}, report)
		assert.True(t, report.Changes())

		composerService.AssertExpectations(t)
		drupalOrgService.AssertExpectations(t)
	})

	t.Run("Current patch fails and no drupalcode client records a conflict", func(t *testing.T) {
		// Without DRUPALCODE_ACCESS_TOKEN the fork lookup is skipped, and the package held back.
		composerService := NewMockPatchesComposer(t)
		drupalOrgService := NewMockDrupalOrg(t)
		var removed []string

		drupalOrgService.EXPECT().FindIssueNumber("Issue #123456 \"With problems\"").Return("123456", true)
		drupalOrgService.EXPECT().GetIssue(anyCtx, "123456").Return(&drupalorg.Issue{
			ID:     "123456",
			Title:  "Alot of problems",
			Status: "1",
			URL:    "https://www.drupal.org/node/123456",
			Project: struct {
				MaschineName string `json:"machine_name"`
			}{MaschineName: "drupal"},
		}, nil)

		composerService.EXPECT().CheckIfPatchApplies(anyCtx, mock.Anything, "drupal/core", "8.8.0", "/tmp/patches/remote/0001-remote.patch").Return(false, nil)

		updater := &ComposerPatches1{
			logger:     logger,
			composer:   composerService,
			drupalOrg:  drupalOrgService,
			drupalCode: nil,
		}

		operations := []composer.PackageChange{
			{Action: "Upgrade", Package: "drupal/core", From: "8.7.0", To: "8.8.0"},
		}
		patches := map[string]map[string]string{
			"drupal/core": {"Issue #123456 \"With problems\"": "patches/remote/0001-remote.patch"},
		}

		updater.removeFile = recordRemovals(&removed)
		report, _ := updater.updatePatches(t.Context(), "/tmp", installedProject(patches), operations, declaredFrom(patches))
		assert.Empty(t, removed)
		require.Len(t, report.Conflicts, 1)
		assert.Equal(t, "drupal/core", report.Conflicts[0].Package)
		assert.Equal(t, "8.7.0", report.Conflicts[0].FixedVersion)
		assert.Equal(t, "8.8.0", report.Conflicts[0].NewVersion)

		composerService.AssertExpectations(t)
		drupalOrgService.AssertExpectations(t)
	})

	t.Run("a failed fork lookup is logged and changes nothing", func(t *testing.T) {
		composerService := NewMockPatchesComposer(t)
		composerService.EXPECT().CheckIfPatchApplies(anyCtx, mock.Anything, "drupal/core", "8.8.0", "/tmp/patches/remote/0001-remote.patch").Return(false, nil)

		drupalOrgService := NewMockDrupalOrg(t)
		drupalOrgService.EXPECT().FindIssueNumber("Issue #123456 \"With problems\"").Return("123456", true)
		drupalOrgService.EXPECT().GetIssue(anyCtx, "123456").Return(&drupalorg.Issue{
			ID: "123456", Title: "Alot of problems", Status: "1", URL: "https://www.drupal.org/node/123456",
			Project: struct {
				MaschineName string `json:"machine_name"`
			}{MaschineName: "drupal"},
		}, nil)

		drupalCode := NewMockDrupalCode(t)
		drupalCode.EXPECT().IssueForkMergeRequests(anyCtx, "drupal", "123456").Return(nil, assert.AnError)

		core, logs := observer.New(zapcore.ErrorLevel)
		var removed []string
		updater := &ComposerPatches1{
			logger:     zap.New(core),
			composer:   composerService,
			drupalOrg:  drupalOrgService,
			drupalCode: drupalCode,
			removeFile: recordRemovals(&removed),
		}
		patches := map[string]map[string]string{
			"drupal/core": {"Issue #123456 \"With problems\"": "patches/remote/0001-remote.patch"},
		}

		report, declared := updater.updatePatches(t.Context(), "/tmp", installedProject(patches), []composer.PackageChange{
			{Action: "Upgrade", Package: "drupal/core", From: "8.7.0", To: "8.8.0"},
		}, declaredFrom(patches))
		newPatches := asMap(declared)

		assert.Empty(t, removed)
		assert.Equal(t, PatchUpdates{}, report)
		assert.Equal(t, map[string]map[string]string{
			"drupal/core": {"Issue #123456: [Alot of problems](https://www.drupal.org/node/123456)": "patches/remote/0001-remote.patch"},
		}, newPatches)
		require.Equal(t, 1, logs.FilterMessage("failed to find the issue fork's merge requests").Len())
	})

	t.Run("Remote patch was committed and released", func(t *testing.T) {

		composerService := NewMockPatchesComposer(t)
		drupalOrgService := NewMockDrupalOrg(t)

		var removed []string

		drupalOrgService.EXPECT().FindIssueNumber("Issue #123456 \"With problems\"").Return("123456", true)
		drupalOrgService.EXPECT().GetIssue(anyCtx, "123456").Return(&drupalorg.Issue{
			ID:     "123456",
			Title:  "Alot of problems",
			Status: "7",
			URL:    "https://www.drupal.org/node/123456",
			Project: struct {
				MaschineName string `json:"machine_name"`
			}{
				MaschineName: "drupal",
			},
		}, nil)

		drupalCode := NewMockDrupalCode(t)
		drupalCode.EXPECT().IsFixedIn(anyCtx, "drupal", "123456", "8.8.0").Return(true, nil)

		updater := &ComposerPatches1{
			logger:     logger,
			composer:   composerService,
			drupalOrg:  drupalOrgService,
			drupalCode: drupalCode,
		}

		operations := []composer.PackageChange{
			{
				Action:  "Upgrade",
				Package: "drupal/core",
				From:    "8.7.0",
				To:      "8.8.0",
			},
		}
		patches := map[string]map[string]string{
			"drupal/core": {
				"Issue #123456 \"With problems\"": "patches/remote/0001-remote.patch",
			},
		}

		updater.removeFile = recordRemovals(&removed)
		report, declared := updater.updatePatches(t.Context(), "/tmp", installedProject(patches), operations, declaredFrom(patches))
		newPatches := asMap(declared)
		assert.Equal(t, []string{"/tmp/patches/remote/0001-remote.patch"}, removed)
		assert.Equal(t, map[string]map[string]string{}, newPatches)
		assert.True(t, report.Changes())

		composerService.AssertExpectations(t)
		drupalOrgService.AssertExpectations(t)
	})

	t.Run("Remote patch was committed, but not yet releases", func(t *testing.T) {

		composerService := NewMockPatchesComposer(t)
		drupalOrgService := NewMockDrupalOrg(t)

		var removed []string

		composerService.EXPECT().CheckIfPatchApplies(anyCtx, mock.Anything, "drupal/core", "8.8.0", "/tmp/patches/remote/0001-remote.patch").Return(true, nil)

		drupalOrgService.EXPECT().FindIssueNumber("Issue #123456 \"With problems\"").Return("123456", true)
		drupalOrgService.EXPECT().GetIssue(anyCtx, "123456").Return(&drupalorg.Issue{
			ID:     "123456",
			Title:  "Alot of problems",
			Status: "7",
			URL:    "https://www.drupal.org/node/123456",
			Project: struct {
				MaschineName string `json:"machine_name"`
			}{
				MaschineName: "drupal",
			},
		}, nil)

		drupalCode := NewMockDrupalCode(t)
		drupalCode.EXPECT().IsFixedIn(anyCtx, "drupal", "123456", "8.8.0").Return(false, nil)

		updater := &ComposerPatches1{
			logger:     logger,
			composer:   composerService,
			drupalOrg:  drupalOrgService,
			drupalCode: drupalCode,
		}

		operations := []composer.PackageChange{
			{
				Action:  "Upgrade",
				Package: "drupal/core",
				From:    "8.7.0",
				To:      "8.8.0",
			},
		}
		patches := map[string]map[string]string{
			"drupal/core": {
				"Issue #123456 \"With problems\"": "patches/remote/0001-remote.patch",
			},
		}

		updater.removeFile = recordRemovals(&removed)
		report, declared := updater.updatePatches(t.Context(), "/tmp", installedProject(patches), operations, declaredFrom(patches))
		newPatches := asMap(declared)
		assert.Empty(t, removed)
		assert.Equal(t, map[string]map[string]string{
			"drupal/core": {
				"Issue #123456: [Alot of problems](https://www.drupal.org/node/123456)": "patches/remote/0001-remote.patch",
			},
		}, newPatches)
		assert.False(t, report.Changes())

		composerService.AssertExpectations(t)
		drupalOrgService.AssertExpectations(t)
	})

	t.Run("Module will be removed", func(t *testing.T) {

		composerService := NewMockPatchesComposer(t)
		drupalOrgService := NewMockDrupalOrg(t)

		var removed []string

		updater := &ComposerPatches1{
			logger:    logger,
			composer:  composerService,
			drupalOrg: drupalOrgService,
		}

		operations := []composer.PackageChange{
			{
				Action:  "Remove",
				Package: "drupal/core",
			},
			{
				Action:  "Remove",
				Package: "drupal/paragraphs",
			},
		}
		patches := map[string]map[string]string{
			"drupal/core": {
				"local patch without issue number": "patches/core/0001-local-patch.patch",
				"remote patch":                     "https://www.drupal.org/node/123456.diff",
			},
			"drupal/pathauto": {
				"local patch without issue number": "patches/core/0001-local-patch.patch",
			},
		}

		updater.removeFile = recordRemovals(&removed)
		report, declared := updater.updatePatches(t.Context(), "/tmp", installedProject(patches), operations, declaredFrom(patches))
		newPatches := asMap(declared)
		assert.Equal(t, []string{"/tmp/patches/core/0001-local-patch.patch"}, removed)
		assert.Equal(t, map[string]map[string]string{
			"drupal/pathauto": {
				"local patch without issue number": "patches/core/0001-local-patch.patch",
			},
		}, newPatches)
		assert.True(t, report.Changes())

		composerService.AssertExpectations(t)
		drupalOrgService.AssertExpectations(t)
	})

	t.Run("Multiple patches conflict when applied together", func(t *testing.T) {
		composerService := NewMockPatchesComposer(t)
		drupalOrgService := NewMockDrupalOrg(t)
		var removed []string

		drupalOrgService.EXPECT().FindIssueNumber("patch one").Return("", false)
		drupalOrgService.EXPECT().FindIssueNumber("patches/core/patch1.patch").Return("", false)
		drupalOrgService.EXPECT().FindIssueNumber("patch two").Return("", false)
		drupalOrgService.EXPECT().FindIssueNumber("patches/core/patch2.patch").Return("", false)

		composerService.EXPECT().CheckIfPatchApplies(anyCtx, mock.Anything, "drupal/core", "8.8.0", "/tmp/patches/core/patch1.patch").Return(true, nil)
		composerService.EXPECT().CheckIfPatchApplies(anyCtx, mock.Anything, "drupal/core", "8.8.0", "/tmp/patches/core/patch2.patch").Return(true, nil)
		composerService.EXPECT().CheckIfPatchesApply(anyCtx, mock.Anything, "drupal/core", "8.8.0", mock.Anything).Return(false, nil)

		updater := &ComposerPatches1{
			logger:    logger,
			composer:  composerService,
			drupalOrg: drupalOrgService,
		}

		operations := []composer.PackageChange{
			{Action: "Upgrade", Package: "drupal/core", From: "8.7.0", To: "8.8.0"},
		}
		patches := map[string]map[string]string{
			"drupal/core": {
				"patch one": "patches/core/patch1.patch",
				"patch two": "patches/core/patch2.patch",
			},
		}

		updater.removeFile = recordRemovals(&removed)
		report, _ := updater.updatePatches(t.Context(), "/tmp", installedProject(patches), operations, declaredFrom(patches))
		assert.Empty(t, removed)
		assert.True(t, report.Changes())
		require.Len(t, report.Conflicts, 1)
		assert.Equal(t, ConflictPatch{
			Package:          "drupal/core",
			FixedVersion:     "8.7.0",
			NewVersion:       "8.8.0",
			PatchDescription: "Multiple patches do not apply together",
		}, report.Conflicts[0], "the description is the reviewer's only clue as to why the version was held back")

		composerService.AssertExpectations(t)
		drupalOrgService.AssertExpectations(t)
	})

	t.Run("Module not installed", func(t *testing.T) {
		composerService := NewMockPatchesComposer(t)
		drupalOrgService := NewMockDrupalOrg(t)

		var removed []string

		updater := &ComposerPatches1{
			logger:    logger,
			composer:  composerService,
			drupalOrg: drupalOrgService,
		}

		operations := []composer.PackageChange{}
		patches := map[string]map[string]string{
			"drupal/core": {
				"local patch without issue number": "patches/core/0001-local-patch.patch",
				"remote patch":                     "https://www.drupal.org/node/123456.diff",
			},
		}

		updater.removeFile = recordRemovals(&removed)
		report, declared := updater.updatePatches(t.Context(), "/tmp", lockedProject(), operations, declaredFrom(patches))
		newPatches := asMap(declared)
		assert.Equal(t, []string{"/tmp/patches/core/0001-local-patch.patch"}, removed)
		assert.Equal(t, map[string]map[string]string{}, newPatches)
		assert.True(t, report.Changes())

		composerService.AssertExpectations(t)
		drupalOrgService.AssertExpectations(t)
	})
}

func TestDownloadFile(t *testing.T) {
	logger := zap.NewNop()

	t.Run("success", func(t *testing.T) {
		const content = "--- a/file\n+++ b/file\n@@ -1 +1 @@\n-old\n+new\n"
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, content)
		}))
		defer server.Close()

		dir := t.TempDir()
		h := &ComposerPatches1{logger: logger, httpClient: server.Client()}

		err := h.downloadFile(t.Context(), server.URL+"/patch.diff", dir, "patch.diff")
		require.NoError(t, err)

		data, err := os.ReadFile(dir + "/patch.diff")
		require.NoError(t, err)
		assert.Equal(t, content, string(data))
	})

	t.Run("http error response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		dir := t.TempDir()
		h := &ComposerPatches1{logger: logger, httpClient: server.Client()}

		err := h.downloadFile(t.Context(), server.URL+"/patch.diff", dir, "patch.diff")
		require.ErrorContains(t, err, "status code 404")
	})

	t.Run("invalid url", func(t *testing.T) {
		h := &ComposerPatches1{logger: logger, httpClient: http.DefaultClient}

		err := h.downloadFile(t.Context(), "not-a-valid-url", t.TempDir(), "patch.diff")
		require.Error(t, err)
	})

	t.Run("mock http client", func(t *testing.T) {
		const content = "patch data"
		mockClient := NewMockHTTPClient(t)
		mockClient.EXPECT().Do(mock.AnythingOfType("*http.Request")).Return(&http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(content)),
		}, nil)

		dir := t.TempDir()
		h := &ComposerPatches1{logger: logger, httpClient: mockClient}

		err := h.downloadFile(t.Context(), "http://example.com/patch.diff", dir, "patch.diff")
		require.NoError(t, err)

		data, err := os.ReadFile(dir + "/patch.diff")
		require.NoError(t, err)
		assert.Equal(t, content, string(data))
	})
}

// installedProject is a project in which every patched package is locked, with no dependency patches.
func installedProject(patches map[string]map[string]string) composer.Project {
	project := composer.Project{Packages: map[string]composer.LockedPackage{}}
	for name := range patches {
		project.Packages[name] = composer.LockedPackage{Version: "1.0.0"}
	}
	return project
}
