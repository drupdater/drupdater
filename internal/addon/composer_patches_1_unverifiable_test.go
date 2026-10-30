package addon

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/drupdater/drupdater/pkg/composer"
	"github.com/drupdater/drupdater/pkg/drupalorg"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.uber.org/zap"
)

// A check that could not run is not a rejected patch. Only the second is a conflict: pinning on
// the first blames a conflict that never happened, on every run.
func TestUpdatePatchesLeavesUnverifiablePatchesUnpinned(t *testing.T) {
	logger := zap.NewNop()
	unverifiable := errors.New("could not obtain drupal/core 8.8.0 to test its patches (not available from any configured repository)")

	operations := []composer.PackageChange{
		{Action: "Upgrade", Package: "drupal/core", From: "8.7.0", To: "8.8.0"},
	}

	t.Run("the declared patch cannot be checked", func(t *testing.T) {
		path := t.TempDir()

		composerService := NewMockPatchesComposer(t)
		composerService.EXPECT().GetDependencyPatches(anyCtx, path).Return(nil, nil).Maybe()
		composerService.EXPECT().IsPackageInstalled(anyCtx, path, "drupal/core").Return(true, nil)
		composerService.EXPECT().
			CheckIfPatchApplies(mock.Anything, path, "drupal/core", "8.8.0", path+"/patches/x.patch").
			Return(false, unverifiable)

		drupalOrgService := NewMockDrupalOrg(t)
		drupalOrgService.EXPECT().FindIssueNumber("local patch").Return("", false)
		drupalOrgService.EXPECT().FindIssueNumber("patches/x.patch").Return("", false)

		updater := &ComposerPatches1{logger: logger, composer: composerService, drupalOrg: drupalOrgService}
		patches := map[string]map[string]string{"drupal/core": {"local patch": "patches/x.patch"}}

		report, newPatches := updater.updatePatches(t.Context(), path, operations, patches)

		assert.Empty(t, report.Conflicts, "an unverifiable patch must not be reported as a conflict")
		assert.False(t, report.Changes())
		assert.Equal(t, map[string]map[string]string{"drupal/core": {"local patch": "patches/x.patch"}}, newPatches,
			"the patch declaration is left exactly as it was")
	})

	t.Run("the replacement patch from the issue fork cannot be checked", func(t *testing.T) {
		path := t.TempDir()

		composerService := NewMockPatchesComposer(t)
		composerService.EXPECT().GetDependencyPatches(anyCtx, path).Return(nil, nil).Maybe()
		composerService.EXPECT().IsPackageInstalled(anyCtx, path, "drupal/core").Return(true, nil)
		// The declared patch is genuinely rejected, so a replacement is fetched from the issue
		// fork -- and checking *that* one is what cannot be carried out.
		composerService.EXPECT().
			CheckIfPatchApplies(mock.Anything, path, "drupal/core", "8.8.0", path+"/patches/remote/0001-remote.patch").
			Return(false, nil)
		composerService.EXPECT().
			CheckIfPatchApplies(mock.Anything, path, "drupal/core", "8.8.0", path+"/patches/drupal/123456-111111-alot_of_problems.diff").
			Return(false, unverifiable)

		drupalOrgService := NewMockDrupalOrg(t)
		drupalOrgService.EXPECT().FindIssueNumber(`Issue #123456 "With problems"`).Return("123456", true)
		drupalOrgService.EXPECT().GetIssue(anyCtx, "123456").Return(&drupalorg.Issue{
			ID:     "123456",
			Title:  "Alot of problems",
			Status: "1",
			URL:    "https://www.drupal.org/node/123456",
			Project: struct {
				MaschineName string `json:"machine_name"`
			}{MaschineName: "drupal"},
		}, nil)

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
		patches := map[string]map[string]string{
			"drupal/core": {`Issue #123456 "With problems"`: "patches/remote/0001-remote.patch"},
		}

		// No removal expected: the replacement is not swapped in, so the old
		// patch file must not be removed either.
		report, newPatches := updater.updatePatches(t.Context(), path, operations, patches)

		assert.Empty(t, report.Conflicts, "an unverifiable replacement must not be reported as a conflict")
		assert.Empty(t, report.Updated)
		assert.Equal(t, map[string]map[string]string{
			"drupal/core": {
				"Issue #123456: [Alot of problems](https://www.drupal.org/node/123456)": "patches/remote/0001-remote.patch",
			},
		}, newPatches, "the package keeps its original patch")
	})
}
