package addon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/drupdater/drupdater/internal"
	"github.com/drupdater/drupdater/pkg/composer"
	"github.com/drupdater/drupdater/pkg/drupalorg"
	git "github.com/go-git/go-git/v5"
	"github.com/gookit/event"
	gitlab "gitlab.com/gitlab-org/api/client-go"
	"go.uber.org/zap"
)

type PatchUpdates struct {
	Removed   []RemovedPatch
	Updated   []UpdatedPatch
	Conflicts []ConflictPatch
}

func (pu PatchUpdates) Changes() bool {
	return len(pu.Removed) > 0 || len(pu.Updated) > 0 || len(pu.Conflicts) > 0
}

type RemovedPatch struct {
	Package          string
	PatchDescription string
	PatchPath        string
	Reason           string
}

type UpdatedPatch struct {
	Package           string
	PatchDescription  string
	PreviousPatchPath string
	NewPatchPath      string
}

type ConflictPatch struct {
	Package          string
	FixedVersion     string
	PatchPath        string
	PatchDescription string
	NewVersion       string
}

type ComposerPatches1 struct {
	BasicAddon
	logger       *zap.Logger
	composer     composer.Runner
	drupalOrg    drupalorg.Client
	gitlab       *gitlab.Client
	patchUpdates PatchUpdates
}

func NewComposerPatches1(logger *zap.Logger, composer composer.Runner, drupalOrg drupalorg.Client) *ComposerPatches1 {

	drupalOrgGitlab, err := gitlab.NewClient(os.Getenv("DRUPALCODE_ACCESS_TOKEN"), gitlab.WithBaseURL("https://git.drupalcode.org/api/v4"))
	if err != nil {
		logger.Error("failed to create gitlab client", zap.Error(err))
	}

	return &ComposerPatches1{
		logger:    logger,
		composer:  composer,
		drupalOrg: drupalOrg,
		gitlab:    drupalOrgGitlab,
	}
}

func (h *ComposerPatches1) SubscribedEvents() map[string]interface{} {
	return map[string]interface{}{
		"pre-composer-update": event.ListenerItem{
			Priority: event.Normal,
			Listener: event.ListenerFunc(h.preComposerUpdateHandler),
		},
	}
}

func (h *ComposerPatches1) RenderTemplate() (string, error) {
	return h.Render("composerpatches.go.tmpl", h.patchUpdates)
}

func (h *ComposerPatches1) preComposerUpdateHandler(e event.Event) error {
	event := e.(*PreComposerUpdateEvent)
	ctx := event.Context()
	path := event.Path()
	worktree := event.Worktree()
	packagesToUpdate := event.PackagesToUpdate
	minimalChanges := event.MinimalChanges

	patches := make(map[string]map[string]string)
	patchesString, err := h.composer.GetConfig(ctx, path, "extra.patches")
	if err != nil {
		h.logger.Debug("extra.patches not defined")
		patchesString = "{}"
	}

	if err := json.Unmarshal([]byte(patchesString), &patches); err != nil {
		return fmt.Errorf("failed to unmarshal patches: %w", err)
	}

	operations, err := h.composer.ListPendingUpdates(ctx, path, packagesToUpdate, minimalChanges)
	if err != nil {
		return fmt.Errorf("failed to get composer updates: %w", err)
	}
	patchUpdates, newPatches := h.UpdatePatches(ctx, path, worktree, operations, patches)
	h.patchUpdates = patchUpdates
	if h.patchUpdates.Changes() {
		// get patches json string
		jsonString, err := json.Marshal(newPatches)
		if err != nil {
			return fmt.Errorf("failed to marshal patches: %w", err)
		}
		err = h.composer.SetConfig(ctx, path, "extra.patches", string(jsonString))
		if err != nil {
			return fmt.Errorf("failed to set composer config: %w", err)
		}

		err = h.composer.UpdateLockHash(ctx, path)
		if err != nil {
			return fmt.Errorf("failed to update composer lock hash: %w", err)
		}

		err = worktree.AddGlob("composer.*")
		if err != nil {
			return fmt.Errorf("failed to add composer.* files: %w", err)
		}
		if _, err := worktree.Commit("Update patches", &git.CommitOptions{}); err != nil {
			return fmt.Errorf("failed to commit patches: %w", err)
		}
	}
	for _, patchUpdate := range patchUpdates.Conflicts {
		event.PackagesToKeep = append(event.PackagesToKeep, fmt.Sprintf("%s:%s", patchUpdate.Package, patchUpdate.FixedVersion))
	}

	return nil
}

func (h *ComposerPatches1) UpdatePatches(ctx context.Context, path string, worktree internal.Worktree, operations []composer.PackageChange, patches map[string]map[string]string) (PatchUpdates, map[string]map[string]string) {

	updates := PatchUpdates{}
	h.logger.Debug("composer patches", zap.Any("patches", patches))

	// Remove patches for packages that are no longer installed
	for packageName := range patches {
		if installed, _ := h.composer.IsPackageInstalled(ctx, path, packageName); !installed {
			for description, patchPath := range patches[packageName] {
				_, err := url.ParseRequestURI(patchPath)
				if err != nil {
					_, err := worktree.Remove(patchPath)
					if err != nil {
						h.logger.Error("failed to remove patch", zap.String("patch", patchPath), zap.Error(err))
					}
				}
				h.logger.Info("removing patch, because it's no longer needed", zap.String("package", packageName), zap.String("patch", patchPath))
				updates.Removed = append(updates.Removed, RemovedPatch{Package: packageName, PatchPath: patchPath, PatchDescription: description, Reason: fmt.Sprintf("%s is not installed in the project", packageName)})
			}
			delete(patches, packageName)
		}
	}

	for _, operation := range operations {

		if operation.Action == "Upgrade" || operation.Action == "Downgrade" {
			if patches[operation.Package] != nil {
				h.logger.Debug("package has patch", zap.String("package", operation.Package))
				for description, patchPath := range patches[operation.Package] {
					issueNumber, issueNumberExists := h.drupalOrg.FindIssueNumber(description)
					if !issueNumberExists {
						issueNumber, issueNumberExists = h.drupalOrg.FindIssueNumber(patchPath)
					}

					if issueNumberExists {
						issue, err := h.drupalOrg.GetIssue(issueNumber)

						if err != nil {
							h.logger.Error("failed to get issue", zap.Error(err))
							continue
						}

						h.logger.Debug("issue", zap.Any("issue", issue))

						delete(patches[operation.Package], description)

						if os.Getenv("DRUPALCODE_ACCESS_TOKEN") == "" {
							h.logger.Debug("skipping issue check, because DRUPALCODE_ACCESS_TOKEN is not set")
						} else {

							// 2 = Fixed, 7 = Closed (fixed), 15 = Patch (to be ported)
							if issue.Status == "2" || issue.Status == "7" || issue.Status == "15" {

								commits, _, err := h.gitlab.Search.CommitsByProject("project/"+issue.Project.MaschineName, issue.ID,
									&gitlab.SearchOptions{
										Ref: &operation.To,
									})

								if err != nil {
									h.logger.Error("failed to search commit history", zap.Error(err))
								} else {
									if len(commits) != 0 {
										h.logger.Debug("issue is fixed", zap.String("issue", issue.ID))
										_, err := worktree.Remove(patchPath)
										if err != nil {
											h.logger.Error("failed to remove patch", zap.Error(err))
										} else {
											if len(patches[operation.Package]) == 0 {
												delete(patches, operation.Package)
											}
											h.logger.Debug("removing patch, because it's no longer needed", zap.String("package", operation.Package), zap.String("patch", patchPath))
											updates.Removed = append(updates.Removed, RemovedPatch{Package: operation.Package, PatchPath: patchPath, Reason: fmt.Sprintf("Issue [#%s](%s) is fixed in %s %s", issue.ID, issue.Title, operation.Package, operation.To), PatchDescription: description})
										}
										continue
									}
								}
							}
						}

						description = "Issue #" + issue.ID + ": [" + issue.Title + "](" + issue.URL + ")"
						patches[operation.Package][description] = patchPath
					}

					// if url is not a valid URL, prepend the path
					absolutePath := patchPath
					externalPatch := true
					_, err := url.ParseRequestURI(patchPath)
					if err != nil {
						externalPatch = false
						absolutePath = path + "/" + patchPath
					}

					if ok, err := h.composer.CheckIfPatchApplies(ctx, operation.Package, operation.To, absolutePath); err != nil {
						h.logger.Debug("failed to check if patch applies", zap.Error(err))
					} else if ok {
						h.logger.Debug("patch applies", zap.String("package", operation.Package), zap.String("version", operation.To), zap.String("patch", patchPath))
					} else {
						h.logger.Debug("patch does not apply", zap.String("package", operation.Package), zap.String("version", operation.To), zap.String("patch", patchPath))
						if issueNumberExists {
							// Download latest patch and try to apply it
							issue, err := h.drupalOrg.GetIssue(issueNumber)
							if err != nil {
								h.logger.Error("failed to get issue", zap.Error(err))
							}

							forkProject, _, err := h.gitlab.Projects.GetProject("issue/"+issue.Project.MaschineName+"-"+issue.ID, &gitlab.GetProjectOptions{})
							if err != nil {
								h.logger.Error("failed to get fork project", zap.Error(err))
							}

							h.logger.Debug("fork project", zap.Any("project", forkProject))

							// We can't use ListMergeRequests here, because it doesn't support filtering by source project
							// and we need to use the fork project as source project.
							opt := struct {
								gitlab.ListProjectMergeRequestsOptions
								SourceProjectID int `url:"source_project_id"`
							}{
								SourceProjectID: forkProject.ID,
							}

							u := "projects/project%2F" + issue.Project.MaschineName + "/merge_requests"

							req, err := h.gitlab.NewRequest(http.MethodGet, u, opt, nil)
							if err != nil {
								continue
							}

							var mergeRequests []*gitlab.BasicMergeRequest
							_, err = h.gitlab.Do(req, &mergeRequests)
							if err != nil {
								continue
							}

							if len(mergeRequests) == 0 {
								h.logger.Debug("no merge requests found")
								//notify
							} else {
								webURL := mergeRequests[0].WebURL
								h.logger.Debug("merge request", zap.String("url", webURL))

								diff := webURL + ".diff"
								newPatchPath := fmt.Sprintf("patches/%s", issue.Project.MaschineName)
								newPatchFile := fmt.Sprintf("%s-%s-%s.diff", issue.ID, mergeRequests[0].SHA, h.cleanURLString(issue.Title))
								h.logger.Debug("downloading patch", zap.String("url", diff), zap.String("path", newPatchPath))
								if err := h.downloadFile(diff, path+"/"+newPatchPath, newPatchFile); err != nil {
									h.logger.Debug("failed to download patch", zap.Error(err))
								}

								if ok, err := h.composer.CheckIfPatchApplies(ctx, operation.Package, operation.To, path+"/"+newPatchPath+"/"+newPatchFile); err != nil {
									h.logger.Debug("failed to check if patch applies", zap.Error(err))
								} else if ok {
									if !externalPatch {
										_, err := worktree.Remove(patchPath)
										if err != nil {
											h.logger.Debug("failed to remove patch", zap.Error(err))
											continue
										}
									}
									patches[operation.Package][description] = newPatchPath + "/" + newPatchFile
									_, err = worktree.Add(newPatchPath + "/" + newPatchFile)
									if err != nil {
										h.logger.Debug("failed to add patch", zap.Error(err))
										continue
									}
									h.logger.Info("replacing patch", zap.String("package", operation.Package), zap.String("previous patch", patchPath), zap.String("new patch", newPatchPath+"/"+newPatchFile))
									updates.Updated = append(updates.Updated, UpdatedPatch{Package: operation.Package, PreviousPatchPath: patchPath, NewPatchPath: newPatchPath + "/" + newPatchFile, PatchDescription: description})
								} else {
									h.logger.Info("merge request does not apply, keeping current package version", zap.String("package", operation.Package), zap.String("version", operation.To), zap.String("patch", path+"/"+newPatchPath))
									updates.Conflicts = append(updates.Conflicts, ConflictPatch{Package: operation.Package, FixedVersion: operation.From, PatchPath: patchPath, NewVersion: operation.To, PatchDescription: description})
								}
							}
						} else {
							// try to get github link from description
							h.logger.Info("patch does not apply, keeping current package version", zap.String("package", operation.Package), zap.String("version", operation.From), zap.String("patch", patchPath))
							updates.Conflicts = append(updates.Conflicts, ConflictPatch{Package: operation.Package, FixedVersion: operation.From, PatchPath: patchPath, NewVersion: operation.To, PatchDescription: description})
						}
					}
				}
			}
		}

		if operation.Action == "Remove" {
			for description, patchPath := range patches[operation.Package] {
				h.logger.Debug("removing patch", zap.String("package", operation.Package), zap.String("patch", patchPath))
				updates.Removed = append(updates.Removed, RemovedPatch{Package: operation.Package, PatchPath: patchPath, PatchDescription: description, Reason: fmt.Sprintf("%s is no longer installed", operation.Package)})
				_, err := url.ParseRequestURI(patchPath)
				if err != nil {
					_, err := worktree.Remove(patches[operation.Package][description])
					if err != nil {
						h.logger.Error("failed to remove patch", zap.Error(err))
					}
				}
			}
			delete(patches, operation.Package)
		}
	}
	h.logger.Debug("composer patches", zap.Any("patches", patches))

	return updates, patches
}

func (h *ComposerPatches1) cleanURLString(s string) string {
	// Replace spaces with underscores
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, " ", "_")

	// Define a regex pattern to keep only URL-valid characters
	re := regexp.MustCompile(`[^a-zA-Z0-9\-._~:/?#\[\]@!$&'()*+,;=]`)
	return re.ReplaceAllString(s, "")
}

// DownloadFile downloads a file from a given URL and saves it to a specified local path.
func (h *ComposerPatches1) downloadFile(url, folder string, file string) error {
	// Get the file from the URL
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Check if the request was successful
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download file: status code %d", resp.StatusCode)
	}

	err = os.MkdirAll(folder, os.ModePerm)
	if err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Create the output file
	outFile, err := os.Create(folder + "/" + file)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer outFile.Close()

	// Copy the response body to the file
	_, err = io.Copy(outFile, resp.Body)
	return err
}
