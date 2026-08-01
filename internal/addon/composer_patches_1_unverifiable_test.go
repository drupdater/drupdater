package addon

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/drupdater/drupdater/pkg/composer"
	"github.com/drupdater/drupdater/pkg/drupalorg"
	gitlab "gitlab.com/gitlab-org/api/client-go"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.uber.org/zap"
)

// A patch check that could not be carried out is not the same as a patch that was checked and
// rejected. Only the second is a conflict; the first must leave the package alone, or a package
// whose registry is unreachable gets pinned on every run and the merge request blames a patch
// conflict that never happened.
func TestUpdatePatchesLeavesUnverifiablePatchesUnpinned(t *testing.T) {
	logger := zap.NewNop()
	unverifiable := errors.New("could not obtain drupal/core 8.8.0 to test its patches (not available from any configured repository)")

	operations := []composer.PackageChange{
		{Action: "Upgrade", Package: "drupal/core", From: "8.7.0", To: "8.8.0"},
	}

	t.Run("the declared patch cannot be checked", func(t *testing.T) {
		path := t.TempDir()

		composerService := NewMockComposer(t)
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

		report, newPatches := updater.updatePatches(t.Context(), path, NewMockWorktree(t), operations, patches)

		assert.Empty(t, report.Conflicts, "an unverifiable patch must not be reported as a conflict")
		assert.False(t, report.Changes())
		assert.Equal(t, map[string]map[string]string{"drupal/core": {"local patch": "patches/x.patch"}}, newPatches,
			"the patch declaration is left exactly as it was")
	})

	t.Run("the replacement patch from the issue fork cannot be checked", func(t *testing.T) {
		path := t.TempDir()

		composerService := NewMockComposer(t)
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

		var serverURL string
		mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/project/drupal/-/merge_requests/1.diff" {
				w.Header().Set("Content-Type", "text/plain")
				_, _ = w.Write([]byte("patch content"))
				return
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)

			var jsonString []byte
			switch r.URL.Path {
			case "/api/v4/projects/issue/drupal-123456":
				jsonString, _ = json.Marshal(&gitlab.Project{ID: 5678})
			case "/api/v4/projects/project/drupal/merge_requests":
				jsonString, _ = json.Marshal([]gitlab.MergeRequest{{
					BasicMergeRequest: gitlab.BasicMergeRequest{
						ID: 1234, IID: 5678, Title: "Remote patch", SHA: "111111",
						WebURL: serverURL + "/project/drupal/-/merge_requests/1",
					},
				}})
			}
			_, err := w.Write(jsonString)
			assert.NoError(t, err)
		}))
		serverURL = mockServer.URL
		defer mockServer.Close()

		gitClient, _ := gitlab.NewClient("", gitlab.WithBaseURL(mockServer.URL))
		updater := &ComposerPatches1{
			logger:     logger,
			composer:   composerService,
			drupalOrg:  drupalOrgService,
			gitlab:     gitClient,
			httpClient: mockServer.Client(),
		}
		patches := map[string]map[string]string{
			"drupal/core": {`Issue #123456 "With problems"`: "patches/remote/0001-remote.patch"},
		}

		// No worktree expectations: the replacement is neither staged nor swapped in, so the old
		// patch file must not be removed either.
		report, newPatches := updater.updatePatches(t.Context(), path, NewMockWorktree(t), operations, patches)

		assert.Empty(t, report.Conflicts, "an unverifiable replacement must not be reported as a conflict")
		assert.Empty(t, report.Updated)
		assert.Equal(t, map[string]map[string]string{
			"drupal/core": {
				"Issue #123456: [Alot of problems](https://www.drupal.org/node/123456)": "patches/remote/0001-remote.patch",
			},
		}, newPatches, "the package keeps its original patch")
	})
}
