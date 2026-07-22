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
	"github.com/drupdater/drupdater/internal/services"
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
	internal.BasicAddon
	logger       *zap.Logger
	composer     Composer
	drupalOrg    DrupalOrg
	gitlab       *gitlab.Client
	httpClient   HTTPClient
	patchUpdates PatchUpdates
}

func NewComposerPatches1(logger *zap.Logger, composer Composer, drupalOrg DrupalOrg, httpClient HTTPClient) *ComposerPatches1 {
	token := os.Getenv("DRUPALCODE_ACCESS_TOKEN")
	var drupalOrgGitlab *gitlab.Client
	if token != "" {
		var err error
		drupalOrgGitlab, err = gitlab.NewClient(token, gitlab.WithBaseURL("https://git.drupalcode.org/api/v4"))
		if err != nil {
			logger.Error("failed to create gitlab client", zap.Error(err))
		}
	}

	return &ComposerPatches1{
		logger:     logger,
		composer:   composer,
		drupalOrg:  drupalOrg,
		gitlab:     drupalOrgGitlab,
		httpClient: httpClient,
	}
}

func (h *ComposerPatches1) SubscribedEvents() map[string]any {
	return map[string]any{
		"pre-composer-update": event.ListenerItem{
			Priority: event.Normal,
			Listener: event.ListenerFunc(h.preComposerUpdateHandler),
		},
	}
}

func (h *ComposerPatches1) RenderTemplate() (string, error) {
	if len(h.patchUpdates.Removed) == 0 && len(h.patchUpdates.Updated) == 0 && len(h.patchUpdates.Conflicts) == 0 {
		return "", nil
	}
	return h.Render("composer_patches_1.go.tmpl", h.patchUpdates)
}

func (h *ComposerPatches1) preComposerUpdateHandler(e event.Event) error {
	event := e.(*services.PreComposerUpdateEvent)
	ctx := event.Context()
	path := event.Path()
	worktree := event.Worktree()
	packagesToUpdate := event.PackagesToUpdate
	minimalChanges := event.MinimalChanges
	packagesToKeep := event.PackagesToKeep

	patches := make(map[string]map[string]string)
	patchesString, err := h.composer.GetConfig(ctx, path, "extra.patches")
	if err != nil {
		h.logger.Debug("extra.patches not defined")
		patchesString = "{}"
	}

	if err := json.Unmarshal([]byte(patchesString), &patches); err != nil {
		return fmt.Errorf("failed to unmarshal patches: %w", err)
	}

	operations, err := h.composer.Update(ctx, path, packagesToUpdate, packagesToKeep, minimalChanges, true)
	if err != nil {
		return fmt.Errorf("failed to get composer updates: %w", err)
	}

	patchUpdates, newPatches := h.updatePatches(ctx, path, worktree, operations, patches)
	h.patchUpdates = patchUpdates

	if h.patchUpdates.Changes() {
		h.logger.Info("patches changed",
			zap.Int("removed", len(h.patchUpdates.Removed)),
			zap.Int("updated", len(h.patchUpdates.Updated)),
			zap.Int("conflicts", len(h.patchUpdates.Conflicts)),
		)

		jsonBytes, err := json.Marshal(newPatches)
		if err != nil {
			return fmt.Errorf("failed to marshal patches: %w", err)
		}

		if err := h.composer.SetConfig(ctx, path, "extra.patches", string(jsonBytes)); err != nil {
			return fmt.Errorf("failed to set composer config: %w", err)
		}

		if err := h.composer.UpdateLockHash(ctx, path); err != nil {
			return fmt.Errorf("failed to update composer lock hash: %w", err)
		}

		if err := worktree.AddGlob("composer.*"); err != nil {
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

func (h *ComposerPatches1) updatePatches(ctx context.Context, path string, worktree Worktree, operations []composer.PackageChange, patches map[string]map[string]string) (PatchUpdates, map[string]map[string]string) {
	updates := PatchUpdates{}
	h.logger.Debug("processing composer patches", zap.Any("patches", patches))

	updates.Removed = append(updates.Removed, h.removeUninstalledPackagePatches(ctx, path, worktree, patches)...)
	updates.Removed = append(updates.Removed, h.removeDependencyProvidedPatches(ctx, path, patches)...)

	for _, op := range operations {
		switch op.Action {
		case "Upgrade", "Downgrade":
			// processSinglePatch mutates patches[op.Package] (it deletes the current key and
			// may insert a rewritten "Issue #..." key). Snapshot the entries first so a newly
			// inserted key isn't visited again in the same pass, which would double-process it.
			for _, e := range snapshotPatches(patches[op.Package]) {
				h.processSinglePatch(ctx, path, worktree, op, e.description, e.patchPath, patches, &updates)
			}
			if len(patches[op.Package]) > 1 {
				h.validateCombinedPatches(ctx, path, op, patches, &updates)
			}
		case "Remove":
			updates.Removed = append(updates.Removed, h.removePackagePatches(worktree, op, patches)...)
		}
	}

	return updates, patches
}

// patchEntry is a single description→path pair snapshotted from a patch map.
type patchEntry struct {
	description string
	patchPath   string
}

// snapshotPatches copies a package's description→path map into a stable slice so callers can
// iterate while mutating the underlying map.
func snapshotPatches(m map[string]string) []patchEntry {
	entries := make([]patchEntry, 0, len(m))
	for description, patchPath := range m {
		entries = append(entries, patchEntry{description: description, patchPath: patchPath})
	}
	return entries
}

// isRemotePatch reports whether a patch reference is a remote (http/https) URL. A bare
// absolute path such as "/patches/x.diff" is a local file, not remote, so scheme is checked
// explicitly rather than relying on url.ParseRequestURI (which accepts absolute paths too).
func isRemotePatch(patchPath string) bool {
	u, err := url.Parse(patchPath)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https")
}

// removeDependencyProvidedPatches drops root patches whose patch file is already applied
// by an installed dependency for the same package. composer-patches collects patches from
// every package, so keeping the root copy makes it apply the same file twice (the second
// fails). Only remote (URL) patches are considered: local paths are package-relative, so a
// matching string across packages would not be the same file.
func (h *ComposerPatches1) removeDependencyProvidedPatches(ctx context.Context, path string, patches map[string]map[string]string) []RemovedPatch {
	depPatches, err := h.composer.GetDependencyPatches(ctx, path)
	if err != nil {
		h.logger.Error("failed to read dependency patches", zap.Error(err))
		return nil
	}

	var removed []RemovedPatch
	for packageName, byDescription := range patches {
		depFiles := depPatches[packageName]
		if depFiles == nil {
			continue
		}
		for description, patchPath := range byDescription {
			if !isRemotePatch(patchPath) {
				continue
			}
			if !depFiles[patchPath] {
				continue
			}
			h.logger.Info("removing patch: already applied by a dependency", zap.String("package", packageName), zap.String("patch", patchPath))
			removed = append(removed, RemovedPatch{Package: packageName, PatchPath: patchPath, PatchDescription: description, Reason: fmt.Sprintf("Patch is already applied by a dependency of %s", packageName)})
			delete(patches[packageName], description)
		}
		if len(patches[packageName]) == 0 {
			delete(patches, packageName)
		}
	}
	return removed
}

func (h *ComposerPatches1) removeUninstalledPackagePatches(ctx context.Context, path string, worktree Worktree, patches map[string]map[string]string) []RemovedPatch {
	var removed []RemovedPatch
	for packageName := range patches {
		if installed, _ := h.composer.IsPackageInstalled(ctx, path, packageName); installed {
			continue
		}
		for description, patchPath := range patches[packageName] {
			if !isRemotePatch(patchPath) {
				if _, err := worktree.Remove(patchPath); err != nil {
					h.logger.Error("failed to remove patch", zap.String("patch", patchPath), zap.Error(err))
				}
			}
			h.logger.Info("removing patch: package no longer installed", zap.String("package", packageName), zap.String("patch", patchPath))
			removed = append(removed, RemovedPatch{Package: packageName, PatchPath: patchPath, PatchDescription: description, Reason: fmt.Sprintf("%s is not installed in the project", packageName)})
		}
		delete(patches, packageName)
	}
	return removed
}

func (h *ComposerPatches1) removePackagePatches(worktree Worktree, op composer.PackageChange, patches map[string]map[string]string) []RemovedPatch {
	var removed []RemovedPatch
	for description, patchPath := range patches[op.Package] {
		h.logger.Debug("removing patch", zap.String("package", op.Package), zap.String("patch", patchPath))
		removed = append(removed, RemovedPatch{Package: op.Package, PatchPath: patchPath, PatchDescription: description, Reason: fmt.Sprintf("%s is no longer installed", op.Package)})
		if !isRemotePatch(patchPath) {
			if _, err := worktree.Remove(patchPath); err != nil {
				h.logger.Error("failed to remove patch", zap.String("patch", patchPath), zap.Error(err))
			}
		}
	}
	delete(patches, op.Package)
	return removed
}

func (h *ComposerPatches1) processSinglePatch(ctx context.Context, path string, worktree Worktree, op composer.PackageChange, description, patchPath string, patches map[string]map[string]string, updates *PatchUpdates) { //nolint:cyclop
	issueNumber, issueNumberExists := h.drupalOrg.FindIssueNumber(description)
	if !issueNumberExists {
		issueNumber, issueNumberExists = h.drupalOrg.FindIssueNumber(patchPath)
	}

	var issue *drupalorg.Issue
	if issueNumberExists {
		var err error
		issue, err = h.drupalOrg.GetIssue(ctx, issueNumber)
		if err != nil {
			h.logger.Error("failed to get issue", zap.String("issue", issueNumber), zap.Error(err))
			return
		}
		h.logger.Debug("fetched issue details", zap.Any("issue", issue))

		delete(patches[op.Package], description)

		// 2 = Fixed, 7 = Closed (fixed), 15 = Patch (to be ported)
		if h.gitlab != nil && (issue.Status == "2" || issue.Status == "7" || issue.Status == "15") {
			commits, _, err := h.gitlab.Search.CommitsByProject("project/"+issue.Project.MaschineName, issue.ID,
				&gitlab.SearchOptions{Ref: &op.To})
			if err != nil {
				h.logger.Error("failed to search commit history", zap.Error(err))
			} else if len(commits) != 0 {
				h.logger.Debug("issue is fixed", zap.String("issue", issue.ID))
				if _, err := worktree.Remove(patchPath); err != nil {
					h.logger.Error("failed to remove patch", zap.String("patch", patchPath), zap.Error(err))
				} else {
					if len(patches[op.Package]) == 0 {
						delete(patches, op.Package)
					}
					h.logger.Info("removing patch: issue fixed in new version", zap.String("package", op.Package), zap.String("patch", patchPath))
					updates.Removed = append(updates.Removed, RemovedPatch{Package: op.Package, PatchPath: patchPath, Reason: fmt.Sprintf("Issue [#%s](%s) is fixed in %s %s", issue.ID, issue.URL, op.Package, op.To), PatchDescription: description})
				}
				return
			}
		}

		description = "Issue #" + issue.ID + ": [" + issue.Title + "](" + issue.URL + ")"
		patches[op.Package][description] = patchPath
	}

	absolutePath := patchPath
	externalPatch := isRemotePatch(patchPath)
	if !externalPatch {
		absolutePath = path + "/" + patchPath
	}

	ok, err := h.composer.CheckIfPatchApplies(ctx, op.Package, op.To, absolutePath)
	if err != nil {
		h.logger.Debug("failed to check if patch applies", zap.Error(err))
		return
	}
	if ok {
		h.logger.Debug("patch applies", zap.String("package", op.Package), zap.String("version", op.To), zap.String("patch", patchPath))
		return
	}

	h.logger.Debug("patch does not apply", zap.String("package", op.Package), zap.String("version", op.To), zap.String("patch", patchPath))

	if !issueNumberExists {
		h.logger.Info("patch does not apply, keeping current package version", zap.String("package", op.Package), zap.String("version", op.From), zap.String("patch", patchPath))
		updates.Conflicts = append(updates.Conflicts, ConflictPatch{Package: op.Package, FixedVersion: op.From, PatchPath: patchPath, NewVersion: op.To, PatchDescription: description})
		return
	}

	// Locating a newer patch from the issue fork needs the drupalcode GitLab client, which is
	// only configured when DRUPALCODE_ACCESS_TOKEN is set. Without it, keep the current version.
	if h.gitlab == nil {
		h.logger.Info("patch does not apply and no drupalcode client is configured, keeping current package version", zap.String("package", op.Package), zap.String("version", op.From), zap.String("patch", patchPath))
		updates.Conflicts = append(updates.Conflicts, ConflictPatch{Package: op.Package, FixedVersion: op.From, PatchPath: patchPath, NewVersion: op.To, PatchDescription: description})
		return
	}

	forkProject, _, err := h.gitlab.Projects.GetProject("issue/"+issue.Project.MaschineName+"-"+issue.ID, &gitlab.GetProjectOptions{})
	if err != nil {
		h.logger.Error("failed to get fork project", zap.Error(err))
		return
	}
	h.logger.Debug("fetched fork project", zap.Any("project", forkProject))

	mergeRequests, err := h.fetchForkMergeRequests(issue.Project.MaschineName, forkProject.ID)
	if err != nil {
		return
	}

	if len(mergeRequests) == 0 {
		h.logger.Debug("no merge requests found")
		return
	}

	mr := mergeRequests[0]
	newPatchDir := fmt.Sprintf("patches/%s", issue.Project.MaschineName)
	newPatchFile := fmt.Sprintf("%s-%s-%s.diff", issue.ID, mr.SHA, h.cleanURLString(issue.Title))
	h.logger.Debug("downloading patch", zap.String("url", mr.WebURL+".diff"), zap.String("path", newPatchDir))

	if err := h.downloadFile(ctx, mr.WebURL+".diff", path+"/"+newPatchDir, newPatchFile); err != nil {
		h.logger.Debug("failed to download patch", zap.Error(err))
		return
	}

	fullNewPath := newPatchDir + "/" + newPatchFile
	if ok, err := h.composer.CheckIfPatchApplies(ctx, op.Package, op.To, path+"/"+fullNewPath); err != nil {
		h.logger.Debug("failed to check if patch applies", zap.String("patch", fullNewPath), zap.Error(err))
		return
	} else if ok {
		if !externalPatch {
			if _, err := worktree.Remove(patchPath); err != nil {
				h.logger.Debug("failed to remove old patch file", zap.String("patch", patchPath), zap.Error(err))
				return
			}
		}
		patches[op.Package][description] = fullNewPath
		if _, err := worktree.Add(fullNewPath); err != nil {
			h.logger.Debug("failed to add patch", zap.Error(err))
			return
		}
		h.logger.Info("replacing patch", zap.String("package", op.Package), zap.String("previous_patch", patchPath), zap.String("new_patch", fullNewPath))
		updates.Updated = append(updates.Updated, UpdatedPatch{Package: op.Package, PreviousPatchPath: patchPath, NewPatchPath: fullNewPath, PatchDescription: description})
	} else {
		h.logger.Info("merge request does not apply, keeping current package version", zap.String("package", op.Package), zap.String("version", op.To), zap.String("patch", path+"/"+newPatchDir))
		updates.Conflicts = append(updates.Conflicts, ConflictPatch{Package: op.Package, FixedVersion: op.From, PatchPath: patchPath, NewVersion: op.To, PatchDescription: description})
	}
}

func (h *ComposerPatches1) validateCombinedPatches(ctx context.Context, path string, op composer.PackageChange, patches map[string]map[string]string, updates *PatchUpdates) {
	patchPaths := make([]string, 0, len(patches[op.Package]))
	for _, patchPath := range patches[op.Package] {
		absolutePath := patchPath
		if _, err := url.ParseRequestURI(patchPath); err != nil {
			absolutePath = path + "/" + patchPath
		}
		patchPaths = append(patchPaths, absolutePath)
	}

	ok, err := h.composer.CheckIfPatchesApply(ctx, op.Package, op.To, patchPaths)
	if err != nil {
		h.logger.Debug("failed to check if patches apply together", zap.Error(err))
		return
	}
	if !ok {
		h.logger.Info("patches do not apply together, keeping current package version",
			zap.String("package", op.Package), zap.String("version", op.To))
		updates.Conflicts = append(updates.Conflicts, ConflictPatch{
			Package:          op.Package,
			FixedVersion:     op.From,
			NewVersion:       op.To,
			PatchDescription: "Multiple patches do not apply together",
		})
	} else {
		h.logger.Debug("patches apply together", zap.String("package", op.Package), zap.String("version", op.To), zap.Any("patch", patchPaths))

	}
}

// fetchForkMergeRequests returns open MRs in projectMachineName that originate from forkProjectID.
// ponytail: raw request because gitlab.ListMergeRequests doesn't support source_project_id filtering
func (h *ComposerPatches1) fetchForkMergeRequests(projectMachineName string, forkProjectID int64) ([]*gitlab.BasicMergeRequest, error) {
	opt := struct {
		gitlab.ListProjectMergeRequestsOptions
		SourceProjectID int64 `url:"source_project_id"`
	}{
		SourceProjectID: forkProjectID,
	}

	u := "projects/project%2F" + projectMachineName + "/merge_requests"
	req, err := h.gitlab.NewRequest(http.MethodGet, u, opt, nil)
	if err != nil {
		return nil, err
	}

	var mergeRequests []*gitlab.BasicMergeRequest
	if _, err = h.gitlab.Do(req, &mergeRequests); err != nil {
		return nil, err
	}
	return mergeRequests, nil
}

// cleanURLString turns an issue title into a safe file name component: lower-cased, spaces
// mapped to underscores, and anything outside [a-z0-9-_.] stripped so the result can never
// contain a path separator or other characters that would break os.Create.
func (h *ComposerPatches1) cleanURLString(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, " ", "_")
	re := regexp.MustCompile(`[^a-zA-Z0-9\-_.]`)
	return re.ReplaceAllString(s, "")
}

func (h *ComposerPatches1) downloadFile(ctx context.Context, url, folder string, file string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download file: status code %d", resp.StatusCode)
	}

	if err = os.MkdirAll(folder, os.ModePerm); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	outFile, err := os.Create(folder + "/" + file)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer outFile.Close()

	_, err = io.Copy(outFile, resp.Body)
	return err
}
