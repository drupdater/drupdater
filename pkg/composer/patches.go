package composer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/spf13/afero"
	"go.uber.org/zap"
)

// GetDependencyPatches returns targetPackage -> patch files declared by installed dependencies,
// which apply on top of the root composer.json patches.
func (s *CLI) GetDependencyPatches(_ context.Context, dir string) (map[string]map[string]bool, error) {
	content, err := afero.ReadFile(s.fs, dir+"/composer.lock")
	if err != nil {
		return nil, fmt.Errorf("failed to read composer.lock: %w", err)
	}

	var lock struct {
		Packages    []lockPackage `json:"packages"`
		PackagesDev []lockPackage `json:"packages-dev"`
	}
	if err := json.Unmarshal(content, &lock); err != nil {
		return nil, fmt.Errorf("failed to unmarshal composer.lock: %w", err)
	}

	result := make(map[string]map[string]bool)
	for _, pkg := range append(lock.Packages, lock.PackagesDev...) {
		// extra is optional and may be serialized as [] when empty; tolerate both.
		var extra struct {
			Patches map[string]map[string]string `json:"patches"`
		}
		if len(pkg.Extra) == 0 || json.Unmarshal(pkg.Extra, &extra) != nil {
			continue
		}
		for targetPackage, byDescription := range extra.Patches {
			for _, file := range byDescription {
				if result[targetPackage] == nil {
					result[targetPackage] = make(map[string]bool)
				}
				result[targetPackage][file] = true
			}
		}
	}
	return result, nil
}

type lockPackage struct {
	Extra json.RawMessage `json:"extra"`
}

// Appended to every scratch project so a contrib module resolves even when the project's own
// repositories are declared somewhere this code cannot read.
const drupalOrgRepositoryURL = "https://packages.drupal.org/8"

// scratchComposerProject is rewritten before every check, so one check leaves no state another reads.
type scratchComposerProject struct {
	Name         string            `json:"name"`
	Type         string            `json:"type"`
	Repositories []json.RawMessage `json:"repositories"`
	Require      map[string]string `json:"require"`
	Config       map[string]any    `json:"config"`
	Extra        map[string]any    `json:"extra"`
}

// buildScratchComposerJSON carries over projectDir's repositories, without which a private
// registry's package is unresolvable and composer_patches misreads that as "does not apply".
//
// The project's repositories come first so a private fork keeps its priority; drupal.org and
// packagist.org stay enabled regardless, which costs nothing and cannot affect the real lock.
func (s *CLI) buildScratchComposerJSON(projectDir string) ([]byte, error) {
	repositories := s.projectRepositories(projectDir)

	// Skip the fallback when the project declares drupal.org itself, so its own entry (which may
	// carry credentials or a mirror URL) is not shadowed.
	if !slices.ContainsFunc(repositories, func(raw json.RawMessage) bool {
		return repositoryURL(raw) == drupalOrgRepositoryURL
	}) {
		repositories = append(repositories, json.RawMessage(
			`{"type":"composer","url":"`+drupalOrgRepositoryURL+`"}`,
		))
	}

	return json.Marshal(scratchComposerProject{
		Name:         "drupdater/patch-test",
		Type:         "project",
		Repositories: repositories,
		Require:      map[string]string{"cweagans/composer-patches": "~1.0"},
		Config:       map[string]any{"allow-plugins": true},
		Extra: map[string]any{
			"composer-exit-on-patch-failure": true,
			"patches-file":                   "composer.patches.json",
		},
	})
}

// projectRepositories returns projectDir's declared repositories. An unreadable composer.json
// yields none rather than an error: fewer repositories beats failing the patch check outright.
func (s *CLI) projectRepositories(projectDir string) []json.RawMessage {
	if projectDir == "" {
		return nil
	}

	content, err := afero.ReadFile(s.fs, filepath.Join(projectDir, "composer.json"))
	if err != nil {
		s.logger.Debug("could not read the project's composer.json for the patch-test project",
			zap.String("dir", projectDir), zap.Error(err))
		return nil
	}

	var project struct {
		Repositories json.RawMessage `json:"repositories"`
	}
	if err := json.Unmarshal(content, &project); err != nil || len(project.Repositories) == 0 {
		return nil
	}

	// composer accepts an array or an object keyed by name; in both, declaration order is
	// resolution priority. It must survive into the scratch project, or a private fork declared
	// ahead of a public repository loses here and its patch is tested against the wrong package.
	var asArray []json.RawMessage
	if err := json.Unmarshal(project.Repositories, &asArray); err != nil {
		var ok bool
		if asArray, ok = orderedObjectValues(project.Repositories); !ok {
			return nil
		}
	}

	repositories := make([]json.RawMessage, 0, len(asArray))
	for _, raw := range asArray {
		if entry, ok := normalizeRepository(raw, projectDir); ok {
			repositories = append(repositories, entry)
		}
	}
	return repositories
}

// orderedObjectValues returns a JSON object's values in declaration order. A map would be
// shorter but loses that order, reordering the project's repositories.
func orderedObjectValues(raw json.RawMessage) ([]json.RawMessage, bool) {
	decoder := json.NewDecoder(bytes.NewReader(raw))

	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return nil, false
	}

	var values []json.RawMessage
	for decoder.More() {
		// Discard the key: composer identifies a repository by type and URL, and the name only
		// matters for overriding an entry.
		if _, err := decoder.Token(); err != nil {
			return nil, false
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, false
		}
		values = append(values, value)
	}

	return values, true
}

// normalizeRepository prepares an entry for the scratch project, false when it must be dropped.
// A disable entry like {"packagist.org": false} goes, since the scratch project needs packagist
// for composer-patches. A "path" repository's relative URL is resolved against the project.
func normalizeRepository(raw json.RawMessage, projectDir string) (json.RawMessage, bool) {
	var entry map[string]any
	if err := json.Unmarshal(raw, &entry); err != nil {
		return nil, false
	}

	// Match the disable form exactly — one name mapped to false — not "contains a boolean": a
	// mirror is declared as {"type":"composer","url":"…","canonical":false}.
	if len(entry) == 1 {
		for _, value := range entry {
			if _, isBool := value.(bool); isBool {
				return nil, false
			}
		}
	}

	url, _ := entry["url"].(string)
	if entry["type"] == "path" && url != "" && !filepath.IsAbs(url) {
		entry["url"] = filepath.Join(projectDir, url)
		normalized, err := json.Marshal(entry)
		if err != nil {
			return nil, false
		}
		return normalized, true
	}

	return raw, true
}

// repositoryURL returns a repository entry's url, or "" when it has none.
func repositoryURL(raw json.RawMessage) string {
	var entry struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(raw, &entry); err != nil {
		return ""
	}
	return entry.URL
}

func (s *CLI) initTempDir() {
	s.tempDir, s.initErr = afero.TempDir(s.fs, "", "composer-service")
}

// resetScratchProject undoes the `composer require` each check leaves behind: without it, a
// conflict between two unrelated checks fails the require and reads as "the patch does not
// apply". composer.json is rebuilt here, not at init, because it carries the project's
// repositories, unknown until a check asks.
func (s *CLI) resetScratchProject(projectDir string) error {
	composerJSON, err := s.buildScratchComposerJSON(projectDir)
	if err != nil {
		return fmt.Errorf("failed to build scratch composer.json: %w", err)
	}
	if err := afero.WriteFile(s.fs, s.tempDir+"/composer.json", composerJSON, 0644); err != nil {
		return fmt.Errorf("failed to reset scratch composer.json: %w", err)
	}
	if err := s.fs.RemoveAll(s.tempDir + "/composer.lock"); err != nil {
		return fmt.Errorf("failed to remove scratch composer.lock: %w", err)
	}
	if err := s.fs.RemoveAll(s.tempDir + "/vendor"); err != nil {
		return fmt.Errorf("failed to remove scratch vendor directory: %w", err)
	}
	return nil
}

// Cleanup removes the patch-test scratch project; defer it, or every run leaks a vendor tree.
func (s *CLI) Cleanup() {
	if s.tempDir == "" {
		return
	}
	if err := s.fs.RemoveAll(s.tempDir); err != nil {
		s.logger.Warn("failed to remove composer scratch directory", zap.String("dir", s.tempDir), zap.Error(err))
	}
	s.tempDir = ""
	// Without resetting, a check after Cleanup skips initTempDir and runs in the process's own
	// working directory (Dir: "" resolves to cwd) instead of a scratch project.
	s.initOnce = sync.Once{}
}

type patchTestConfig struct {
	Patches map[string]map[string]string `json:"patches"`
}

// CheckIfPatchApplies reports whether one patch still applies to packageName at packageVersion.
func (s *CLI) CheckIfPatchApplies(ctx context.Context, projectDir string, packageName string, packageVersion string, patchPath string) (bool, error) {
	return s.CheckIfPatchesApply(ctx, projectDir, packageName, packageVersion, []string{patchPath})
}

// requireIntoScratch installs the package under test: true when it installed, false when the
// patch was rejected, error when composer never got the package. The third case must stay
// distinct — false pins the package, which is wrong for an unreachable registry. Not --quiet:
// that suppresses the message this classification reads.
func (s *CLI) requireIntoScratch(ctx context.Context, packageName string, packageVersion string) (bool, error) {
	out, err := s.execComposer(ctx, s.tempDir,
		"require", "--ignore-platform-reqs", packageName+":"+constraint(packageVersion), "--with-all-dependencies")
	if err == nil {
		return true, nil
	}

	if reason, unresolvable := unresolvableReason(out); unresolvable {
		return false, fmt.Errorf("could not obtain %s %s to test its patches (%s): %w", packageName, packageVersion, reason, err)
	}

	return false, nil //nolint:nilerr // composer obtained the package and the patch was rejected
}

// unresolvableReason reports whether composer could not obtain the package, and why. A rejected
// patch wins outright: the dist-to-source fallback logs "could not be downloaded (404)" and then
// succeeds, so the unresolvable patterns alone would misread a genuine rejection.
//
// Both match composer's English output, so prefer adding a pattern to loosening one.
func unresolvableReason(out string) (string, bool) {
	lower := strings.ToLower(out)

	// The two wordings composer-patches v1 uses for a patch failure.
	if strings.Contains(lower, "could not apply patch") || strings.Contains(lower, "cannot apply patch") {
		return "", false
	}

	for _, candidate := range unresolvablePatterns {
		if strings.Contains(lower, candidate.pattern) {
			return candidate.reason, true
		}
	}
	return "", false
}

var unresolvablePatterns = []struct{ pattern, reason string }{
	{"could not find package", "not available from any configured repository"},
	{"could not find a matching version", "not available from any configured repository"},
	{"could not be found", "not available from any configured repository"},
	{"invalid credentials", "repository authentication failed"},
	{"authentication required", "repository authentication failed"},
	{"could not be downloaded", "a required download failed"},
}

// CheckIfPatchesApply reports whether patchPaths all apply together. The keys are zero-padded
// indexes because composer-patches v1 applies patches in key order.
func (s *CLI) CheckIfPatchesApply(ctx context.Context, projectDir string, packageName string, packageVersion string, patchPaths []string) (bool, error) {
	s.initOnce.Do(s.initTempDir)
	if s.initErr != nil {
		return false, s.initErr
	}
	if err := s.resetScratchProject(projectDir); err != nil {
		return false, err
	}

	patchMap := make(map[string]string, len(patchPaths))
	for i, p := range patchPaths {
		patchMap[fmt.Sprintf("%010d", i)] = p
	}

	// Marshalled rather than formatted, so special characters in the arguments stay escaped.
	patchesJSONBytes, err := json.Marshal(patchTestConfig{
		Patches: map[string]map[string]string{packageName: patchMap},
	})
	if err != nil {
		return false, fmt.Errorf("failed to marshal patch config: %w", err)
	}

	if err := afero.WriteFile(s.fs, s.tempDir+"/composer.patches.json", patchesJSONBytes, 0644); err != nil {
		return false, err
	}

	return s.requireIntoScratch(ctx, packageName, packageVersion)
}
