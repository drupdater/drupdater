package services

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
	"github.com/drupdater/drupdater/internal/addon"
	"github.com/drupdater/drupdater/pkg/composer"
	"github.com/drupdater/drupdater/pkg/drupal"
	"github.com/drupdater/drupdater/pkg/drupalorg"
	"github.com/drupdater/drupdater/pkg/drush"
	"github.com/drupdater/drupdater/pkg/repo"
	git "github.com/go-git/go-git/v5"
	"github.com/gookit/event"
	gitlab "gitlab.com/gitlab-org/api/client-go"
	"go.uber.org/zap"
)

type DependencyUpdateReport struct {
	AddedAllowPlugins []string
	PatchUpdates      PatchUpdates
}

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

type UpdaterService interface {
	UpdateDependencies(ctx context.Context, path string, packagesToUpdate []string, worktree internal.Worktree, minimalChanges bool) (DependencyUpdateReport, error)
	UpdateDrupal(ctx context.Context, path string, worktree internal.Worktree, site string) (map[string]drush.UpdateHook, error)
	UpdatePatches(ctx context.Context, path string, worktree internal.Worktree, operations []composer.PackageChange, patches map[string]map[string]string) (PatchUpdates, map[string]map[string]string)
}

type DefaultUpdater struct {
	logger     *zap.Logger
	settings   drupal.SettingsService
	repository repo.RepositoryService
	config     internal.Config
	composer   composer.Runner
	drupalOrg  drupalorg.Client
	gitlab     *gitlab.Client
	drush      drush.Runner
}

func NewDefaultUpdater(logger *zap.Logger, settings drupal.SettingsService, repository repo.RepositoryService, config internal.Config, composer composer.Runner, drupalOrg drupalorg.Client, drush drush.Runner) *DefaultUpdater {

	drupalOrgGitlab, err := gitlab.NewClient(os.Getenv("DRUPALCODE_ACCESS_TOKEN"), gitlab.WithBaseURL("https://git.drupalcode.org/api/v4"))
	if err != nil {
		logger.Error("failed to create gitlab client", zap.Error(err))
	}

	return &DefaultUpdater{
		logger:     logger,
		settings:   settings,
		repository: repository,
		config:     config,
		composer:   composer,
		drupalOrg:  drupalOrg,
		gitlab:     drupalOrgGitlab,
		drush:      drush,
	}
}

func (us *DefaultUpdater) UpdateDependencies(ctx context.Context, path string, packagesToUpdate []string, worktree internal.Worktree, minimalChanges bool) (DependencyUpdateReport, error) {
	var updateReport DependencyUpdateReport

	patches := make(map[string]map[string]string)
	patchesString, err := us.composer.GetConfig(ctx, path, "extra.patches")
	if err != nil {
		us.logger.Debug("extra.patches not defined")
		patchesString = "{}"
	}

	if err := json.Unmarshal([]byte(patchesString), &patches); err != nil {
		return updateReport, fmt.Errorf("failed to unmarshal patches: %w", err)
	}

	operations, err := us.composer.ListPendingUpdates(ctx, path, packagesToUpdate, minimalChanges)
	if err != nil {
		return updateReport, fmt.Errorf("failed to get composer updates: %w", err)
	}
	patchUpdates, newPatches := us.UpdatePatches(ctx, path, worktree, operations, patches)
	updateReport.PatchUpdates = patchUpdates
	if updateReport.PatchUpdates.Changes() {
		// get patches json string
		jsonString, err := json.Marshal(newPatches)
		if err != nil {
			return updateReport, fmt.Errorf("failed to marshal patches: %w", err)
		}
		err = us.composer.SetConfig(ctx, path, "extra.patches", string(jsonString))
		if err != nil {
			return updateReport, fmt.Errorf("failed to set composer config: %w", err)
		}

		err = us.composer.UpdateLockHash(ctx, path)
		if err != nil {
			return updateReport, fmt.Errorf("failed to update composer lock hash: %w", err)
		}

		err = worktree.AddGlob("composer.*")
		if err != nil {
			return updateReport, fmt.Errorf("failed to add composer.* files: %w", err)
		}
		if _, err := worktree.Commit("Update patches", &git.CommitOptions{}); err != nil {
			return updateReport, fmt.Errorf("failed to commit patches: %w", err)
		}
	}

	allowPlugins, err := us.composer.GetAllowPlugins(ctx, path)
	if err != nil {
		return updateReport, fmt.Errorf("failed to get composer allow plugins: %w", err)
	}

	// Allow all plugins during update
	err = us.composer.SetConfig(ctx, path, "allow-plugins", "true")
	if err != nil {
		return updateReport, fmt.Errorf("failed to set composer config: %w", err)
	}

	packagesToKeep := []string{}
	for _, patchUpdate := range patchUpdates.Conflicts {
		packagesToKeep = append(packagesToKeep, fmt.Sprintf("%s:%s", patchUpdate.Package, patchUpdate.FixedVersion))
	}
	if _, err = us.composer.Update(ctx, path, packagesToUpdate, packagesToKeep, minimalChanges, false); err != nil {
		return updateReport, err
	}

	allPlugins, err := us.composer.GetInstalledPlugins(ctx, path)
	if err != nil {
		return updateReport, err
	}

	// Add new plugins to allow-plugins
	for key := range allPlugins {
		if _, ok := allowPlugins[key]; !ok {
			allowPlugins[key] = false
			updateReport.AddedAllowPlugins = append(updateReport.AddedAllowPlugins, key)
		}
	}
	if err := us.composer.SetAllowPlugins(ctx, path, allowPlugins); err != nil {
		return updateReport, err
	}

	if _, err := us.composer.Normalize(ctx, path); err != nil {
		us.logger.Debug("failed to run composer normalize", zap.Error(err))
	}

	err = worktree.AddGlob("composer.*")
	if err != nil {
		return updateReport, fmt.Errorf("failed to add composer.* files: %w", err)
	}
	if _, err := worktree.Commit("Update composer.json and composer.lock", &git.CommitOptions{}); err != nil {
		return updateReport, fmt.Errorf("failed to commit composer.json and composer.lock: %w", err)
	}

	return updateReport, nil
}

type UpdateHooksPerSite map[string]map[string]drush.UpdateHook

func (us *DefaultUpdater) UpdateDrupal(ctx context.Context, path string, worktree internal.Worktree, site string) (map[string]drush.UpdateHook, error) {

	us.logger.Info("updating site", zap.String("site", site))

	if err := us.settings.ConfigureDatabase(ctx, path, site); err != nil {
		return nil, fmt.Errorf("failed to configure database: %w", err)
	}

	hooks, err := us.drush.GetUpdateHooks(ctx, path, site)
	us.logger.Debug("update hooks", zap.Any("hooks", hooks))
	if err != nil {
		return nil, fmt.Errorf("failed to get update hooks: %w", err)

	}

	if err := us.drush.UpdateSite(ctx, path, site); err != nil {
		return nil, fmt.Errorf("failed to update site: %w", err)

	}

	if err := us.drush.ConfigResave(ctx, path, site); err != nil {
		return nil, fmt.Errorf("failed to resave config: %w", err)

	}

	e := &addon.PostSiteUpdate{
		Ctx:      ctx,
		Worktree: worktree,
		Path:     path,
		Site:     site,
	}
	e.SetName("post-site-update")
	event.AddEvent(e)

	if err := event.FireEvent(e); err != nil {
		return nil, fmt.Errorf("failed to fire event: %w", err)
	}

	us.logger.Info("export configuration", zap.String("site", site))
	if err := us.drush.ExportConfiguration(ctx, path, site); err != nil {
		return nil, fmt.Errorf("failed to export configuration: %w", err)
	}

	return hooks, nil
}

func (us *DefaultUpdater) UpdatePatches(ctx context.Context, path string, worktree internal.Worktree, operations []composer.PackageChange, patches map[string]map[string]string) (PatchUpdates, map[string]map[string]string) {

	updates := PatchUpdates{}
	us.logger.Debug("composer patches", zap.Any("patches", patches))

	// Remove patches for packages that are no longer installed
	for packageName := range patches {
		if installed, _ := us.composer.IsPackageInstalled(ctx, path, packageName); !installed {
			for description, patchPath := range patches[packageName] {
				_, err := url.ParseRequestURI(patchPath)
				if err != nil {
					_, err := worktree.Remove(patchPath)
					if err != nil {
						us.logger.Error("failed to remove patch", zap.String("patch", patchPath), zap.Error(err))
					}
				}
				us.logger.Info("removing patch, because it's no longer needed", zap.String("package", packageName), zap.String("patch", patchPath))
				updates.Removed = append(updates.Removed, RemovedPatch{Package: packageName, PatchPath: patchPath, PatchDescription: description, Reason: fmt.Sprintf("%s is not installed in the project", packageName)})
			}
			delete(patches, packageName)
		}
	}

	for _, operation := range operations {

		if operation.Action == "Upgrade" || operation.Action == "Downgrade" {
			if patches[operation.Package] != nil {
				us.logger.Debug("package has patch", zap.String("package", operation.Package))
				for description, patchPath := range patches[operation.Package] {
					issueNumber, issueNumberExists := us.drupalOrg.FindIssueNumber(description)
					if !issueNumberExists {
						issueNumber, issueNumberExists = us.drupalOrg.FindIssueNumber(patchPath)
					}

					if issueNumberExists {
						issue, err := us.drupalOrg.GetIssue(issueNumber)

						if err != nil {
							us.logger.Error("failed to get issue", zap.Error(err))
							continue
						}

						us.logger.Debug("issue", zap.Any("issue", issue))

						delete(patches[operation.Package], description)

						if os.Getenv("DRUPALCODE_ACCESS_TOKEN") == "" {
							us.logger.Debug("skipping issue check, because DRUPALCODE_ACCESS_TOKEN is not set")
						} else {

							// 2 = Fixed, 7 = Closed (fixed), 15 = Patch (to be ported)
							if issue.Status == "2" || issue.Status == "7" || issue.Status == "15" {

								commits, _, err := us.gitlab.Search.CommitsByProject("project/"+issue.Project.MaschineName, issue.ID,
									&gitlab.SearchOptions{
										Ref: &operation.To,
									})

								if err != nil {
									us.logger.Error("failed to search commit history", zap.Error(err))
								} else {
									if len(commits) != 0 {
										us.logger.Debug("issue is fixed", zap.String("issue", issue.ID))
										_, err := worktree.Remove(patchPath)
										if err != nil {
											us.logger.Error("failed to remove patch", zap.Error(err))
										} else {
											if len(patches[operation.Package]) == 0 {
												delete(patches, operation.Package)
											}
											us.logger.Debug("removing patch, because it's no longer needed", zap.String("package", operation.Package), zap.String("patch", patchPath))
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

					if ok, err := us.composer.CheckIfPatchApplies(ctx, operation.Package, operation.To, absolutePath); err != nil {
						us.logger.Debug("failed to check if patch applies", zap.Error(err))
					} else if ok {
						us.logger.Debug("patch applies", zap.String("package", operation.Package), zap.String("version", operation.To), zap.String("patch", patchPath))
					} else {
						us.logger.Debug("patch does not apply", zap.String("package", operation.Package), zap.String("version", operation.To), zap.String("patch", patchPath))
						if issueNumberExists {
							// Download latest patch and try to apply it
							issue, err := us.drupalOrg.GetIssue(issueNumber)
							if err != nil {
								us.logger.Error("failed to get issue", zap.Error(err))
							}

							forkProject, _, err := us.gitlab.Projects.GetProject("issue/"+issue.Project.MaschineName+"-"+issue.ID, &gitlab.GetProjectOptions{})
							if err != nil {
								us.logger.Error("failed to get fork project", zap.Error(err))
							}

							us.logger.Debug("fork project", zap.Any("project", forkProject))

							// We can't use ListMergeRequests here, because it doesn't support filtering by source project
							// and we need to use the fork project as source project.
							opt := struct {
								gitlab.ListProjectMergeRequestsOptions
								SourceProjectID int `url:"source_project_id"`
							}{
								SourceProjectID: forkProject.ID,
							}

							u := "projects/project%2F" + issue.Project.MaschineName + "/merge_requests"

							req, err := us.gitlab.NewRequest(http.MethodGet, u, opt, nil)
							if err != nil {
								continue
							}

							var mergeRequests []*gitlab.BasicMergeRequest
							_, err = us.gitlab.Do(req, &mergeRequests)
							if err != nil {
								continue
							}

							if len(mergeRequests) == 0 {
								us.logger.Debug("no merge requests found")
								//notify
							} else {
								webURL := mergeRequests[0].WebURL
								us.logger.Debug("merge request", zap.String("url", webURL))

								diff := webURL + ".diff"
								newPatchPath := fmt.Sprintf("patches/%s", issue.Project.MaschineName)
								newPatchFile := fmt.Sprintf("%s-%s-%s.diff", issue.ID, mergeRequests[0].SHA, us.cleanURLString(issue.Title))
								us.logger.Debug("downloading patch", zap.String("url", diff), zap.String("path", newPatchPath))
								if err := us.downloadFile(diff, path+"/"+newPatchPath, newPatchFile); err != nil {
									us.logger.Debug("failed to download patch", zap.Error(err))
								}

								if ok, err := us.composer.CheckIfPatchApplies(ctx, operation.Package, operation.To, path+"/"+newPatchPath+"/"+newPatchFile); err != nil {
									us.logger.Debug("failed to check if patch applies", zap.Error(err))
								} else if ok {
									if !externalPatch {
										_, err := worktree.Remove(patchPath)
										if err != nil {
											us.logger.Debug("failed to remove patch", zap.Error(err))
											continue
										}
									}
									patches[operation.Package][description] = newPatchPath + "/" + newPatchFile
									_, err = worktree.Add(newPatchPath + "/" + newPatchFile)
									if err != nil {
										us.logger.Debug("failed to add patch", zap.Error(err))
										continue
									}
									us.logger.Info("replacing patch", zap.String("package", operation.Package), zap.String("previous patch", patchPath), zap.String("new patch", newPatchPath+"/"+newPatchFile))
									updates.Updated = append(updates.Updated, UpdatedPatch{Package: operation.Package, PreviousPatchPath: patchPath, NewPatchPath: newPatchPath + "/" + newPatchFile, PatchDescription: description})
								} else {
									us.logger.Info("merge request does not apply, keeping current package version", zap.String("package", operation.Package), zap.String("version", operation.To), zap.String("patch", path+"/"+newPatchPath))
									updates.Conflicts = append(updates.Conflicts, ConflictPatch{Package: operation.Package, FixedVersion: operation.From, PatchPath: patchPath, NewVersion: operation.To, PatchDescription: description})
								}
							}
						} else {
							// try to get github link from description
							us.logger.Info("patch does not apply, keeping current package version", zap.String("package", operation.Package), zap.String("version", operation.From), zap.String("patch", patchPath))
							updates.Conflicts = append(updates.Conflicts, ConflictPatch{Package: operation.Package, FixedVersion: operation.From, PatchPath: patchPath, NewVersion: operation.To, PatchDescription: description})
						}
					}
				}
			}
		}

		if operation.Action == "Remove" {
			for description, patchPath := range patches[operation.Package] {
				us.logger.Debug("removing patch", zap.String("package", operation.Package), zap.String("patch", patchPath))
				updates.Removed = append(updates.Removed, RemovedPatch{Package: operation.Package, PatchPath: patchPath, PatchDescription: description, Reason: fmt.Sprintf("%s is no longer installed", operation.Package)})
				_, err := url.ParseRequestURI(patchPath)
				if err != nil {
					_, err := worktree.Remove(patches[operation.Package][description])
					if err != nil {
						us.logger.Error("failed to remove patch", zap.Error(err))
					}
				}
			}
			delete(patches, operation.Package)
		}
	}
	us.logger.Debug("composer patches", zap.Any("patches", patches))

	return updates, patches
}

func (us *DefaultUpdater) cleanURLString(s string) string {
	// Replace spaces with underscores
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, " ", "_")

	// Define a regex pattern to keep only URL-valid characters
	re := regexp.MustCompile(`[^a-zA-Z0-9\-._~:/?#\[\]@!$&'()*+,;=]`)
	return re.ReplaceAllString(s, "")
}

// DownloadFile downloads a file from a given URL and saves it to a specified local path.
func (us *DefaultUpdater) downloadFile(url, folder string, file string) error {
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
