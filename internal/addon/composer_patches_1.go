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

// The json tags below are published --report schema; renaming one needs a SchemaVersion bump.

type RemovedPatch struct {
	Package          string `json:"package"`
	PatchDescription string `json:"patch_description"`
	PatchPath        string `json:"patch_path"`
	Reason           string `json:"reason"`
}

type UpdatedPatch struct {
	Package           string `json:"package"`
	PatchDescription  string `json:"patch_description"`
	PreviousPatchPath string `json:"previous_patch_path"`
	NewPatchPath      string `json:"new_patch_path"`
}

type ConflictPatch struct {
	Package          string `json:"package"`
	FixedVersion     string `json:"fixed_version"`
	PatchPath        string `json:"patch_path"`
	PatchDescription string `json:"patch_description"`
	NewVersion       string `json:"new_version"`
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
			// processSinglePatch mutates patches[op.Package]; a key it inserts must not be
			// visited again in the same pass.
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

// snapshotPatches copies a patch map so callers can iterate while mutating the underlying map.
func snapshotPatches(m map[string]string) []patchEntry {
	entries := make([]patchEntry, 0, len(m))
	for description, patchPath := range m {
		entries = append(entries, patchEntry{description: description, patchPath: patchPath})
	}
	return entries
}

// isRemotePatch checks the scheme explicitly: url.ParseRequestURI also accepts "/patches/x.diff".
func isRemotePatch(patchPath string) bool {
	u, err := url.Parse(patchPath)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https")
}

// resolvePatchPath makes a patch reference absolute for the patch-test project, which runs from a
// temp directory where a project-relative path points nowhere.
func resolvePatchPath(projectDir string, patchPath string) string {
	if isRemotePatch(patchPath) {
		return patchPath
	}
	return projectDir + "/" + patchPath
}

// conflict records a package held back at its current version because a patch no longer applies.
func conflict(op composer.PackageChange, patchPath string, description string) ConflictPatch {
	return ConflictPatch{
		Package:          op.Package,
		FixedVersion:     op.From,
		NewVersion:       op.To,
		PatchPath:        patchPath,
		PatchDescription: description,
	}
}

// dropPatchFile removes a patch's file. Every removal goes through here so a remote patch's URL
// never reaches worktree.Remove, which would fail and read as "the patch could not be dropped".
func (h *ComposerPatches1) dropPatchFile(worktree Worktree, patchPath string) error {
	if isRemotePatch(patchPath) {
		return nil
	}
	_, err := worktree.Remove(patchPath)
	return err
}

// removeDependencyProvidedPatches drops root patches a dependency already applies, which
// composer-patches would apply twice. Remote only: a local path is package-relative.
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
			if err := h.dropPatchFile(worktree, patchPath); err != nil {
				h.logger.Error("failed to remove patch", zap.String("patch", patchPath), zap.Error(err))
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
		if err := h.dropPatchFile(worktree, patchPath); err != nil {
			h.logger.Error("failed to remove patch", zap.String("patch", patchPath), zap.Error(err))
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
				if err := h.dropPatchFile(worktree, patchPath); err != nil {
					// Restore the entry deleted above: a patch whose file survived must stay
					// declared rather than vanish from composer.json unreported.
					h.logger.Error("failed to remove patch", zap.String("patch", patchPath), zap.Error(err))
					patches[op.Package][description] = patchPath
					return
				}
				if len(patches[op.Package]) == 0 {
					delete(patches, op.Package)
				}
				h.logger.Info("removing patch: issue fixed in new version", zap.String("package", op.Package), zap.String("patch", patchPath))
				updates.Removed = append(updates.Removed, RemovedPatch{Package: op.Package, PatchPath: patchPath, Reason: fmt.Sprintf("Issue [#%s](%s) is fixed in %s %s", issue.ID, issue.URL, op.Package, op.To), PatchDescription: description})
				return
			}
		}

		description = "Issue #" + issue.ID + ": [" + issue.Title + "](" + issue.URL + ")"
		patches[op.Package][description] = patchPath
	}

	ok, err := h.composer.CheckIfPatchApplies(ctx, path, op.Package, op.To, resolvePatchPath(path, patchPath))
	if err != nil {
		// An unverifiable patch is not a stale one: pinning here would hold the package back
		// on every run and blame a conflict that never happened.
		h.logger.Warn("could not check whether the patch still applies, leaving the package unpinned",
			zap.String("package", op.Package), zap.String("patch", patchPath), zap.Error(err))
		return
	}
	if ok {
		h.logger.Debug("patch applies", zap.String("package", op.Package), zap.String("version", op.To), zap.String("patch", patchPath))
		return
	}

	h.logger.Debug("patch does not apply", zap.String("package", op.Package), zap.String("version", op.To), zap.String("patch", patchPath))

	if !issueNumberExists {
		h.logger.Info("patch does not apply, keeping current package version", zap.String("package", op.Package), zap.String("version", op.From), zap.String("patch", patchPath))
		updates.Conflicts = append(updates.Conflicts, conflict(op, patchPath, description))
		return
	}

	// Finding a newer patch needs the drupalcode client, configured only with DRUPALCODE_ACCESS_TOKEN.
	if h.gitlab == nil {
		h.logger.Info("patch does not apply and no drupalcode client is configured, keeping current package version", zap.String("package", op.Package), zap.String("version", op.From), zap.String("patch", patchPath))
		updates.Conflicts = append(updates.Conflicts, conflict(op, patchPath, description))
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
	if ok, err := h.composer.CheckIfPatchApplies(ctx, path, op.Package, op.To, path+"/"+fullNewPath); err != nil {
		h.logger.Warn("could not check whether the merge request patch applies, leaving the package unpinned",
			zap.String("package", op.Package), zap.String("patch", fullNewPath), zap.Error(err))
		return
	} else if ok {
		if err := h.dropPatchFile(worktree, patchPath); err != nil {
			h.logger.Debug("failed to remove old patch file", zap.String("patch", patchPath), zap.Error(err))
			return
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
		updates.Conflicts = append(updates.Conflicts, conflict(op, patchPath, description))
	}
}

func (h *ComposerPatches1) validateCombinedPatches(ctx context.Context, path string, op composer.PackageChange, patches map[string]map[string]string, updates *PatchUpdates) {
	patchPaths := make([]string, 0, len(patches[op.Package]))
	for _, patchPath := range patches[op.Package] {
		patchPaths = append(patchPaths, resolvePatchPath(path, patchPath))
	}

	ok, err := h.composer.CheckIfPatchesApply(ctx, path, op.Package, op.To, patchPaths)
	if err != nil {
		h.logger.Warn("could not check whether the patches apply together, leaving the package unpinned",
			zap.String("package", op.Package), zap.Error(err))
		return
	}
	if !ok {
		h.logger.Info("patches do not apply together, keeping current package version",
			zap.String("package", op.Package), zap.String("version", op.To))
		// No single patch to name: the package is held back because the set as a whole failed.
		updates.Conflicts = append(updates.Conflicts, conflict(op, "", "Multiple patches do not apply together"))
	} else {
		h.logger.Debug("patches apply together", zap.String("package", op.Package), zap.String("version", op.To), zap.Any("patch", patchPaths))

	}
}

// fetchForkMergeRequests returns open MRs in projectMachineName originating from forkProjectID.
// Raw request: gitlab.ListMergeRequests cannot filter on source_project_id.
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

var unsafeFileNameChars = regexp.MustCompile(`[^a-zA-Z0-9\-_.]`)

// cleanURLString turns an issue title into a file name component that holds no path separator.
func (h *ComposerPatches1) cleanURLString(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, " ", "_")
	return unsafeFileNameChars.ReplaceAllString(s, "")
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

	if err = os.MkdirAll(folder, 0o755); err != nil {
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
