package composer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/spf13/afero"
	"go.uber.org/zap"
)

var execCommand = exec.CommandContext

type CLI struct {
	fs     afero.Fs
	logger *zap.Logger

	tempDir  string
	initOnce sync.Once
	initErr  error
}

func NewCLI(logger *zap.Logger) *CLI {
	return &CLI{
		fs:     afero.NewOsFs(),
		logger: logger,
	}
}

func (s *CLI) execComposer(ctx context.Context, dir string, args ...string) (string, error) {
	command := execCommand(ctx, "composer", args...)
	command.Dir = dir

	out, err := command.CombinedOutput()
	output := strings.TrimSuffix(string(out), "\n")
	s.logger.Debug(command.String() + "\n" + output)

	return output, err
}

// execComposerJSON runs composer and returns only stdout, keeping stderr out of the result.
// Commands whose output is parsed as JSON must use this: composer prints warnings and notices
// (e.g. "Composer plugins have been disabled") to stderr, and folding them into stdout would
// corrupt the JSON. stderr is still captured for the debug log.
func (s *CLI) execComposerJSON(ctx context.Context, dir string, args ...string) (string, error) {
	command := execCommand(ctx, "composer", args...)
	command.Dir = dir

	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()

	output := strings.TrimSuffix(stdout.String(), "\n")
	s.logger.Debug(command.String() + "\nstdout: " + output + "\nstderr: " + strings.TrimSuffix(stderr.String(), "\n"))

	return output, err
}

// PackageChange represents an individual package operation
type PackageChange struct {
	Action  string // Install, Upgrade, Remove, Downgrade
	Package string
	From    string
	To      string
}

func (s *CLI) Update(ctx context.Context, dir string, packages []string, packagesToKeep []string, minimalChanges bool, dryRun bool) ([]PackageChange, error) {
	args := append([]string{"update", "--no-interaction", "--no-progress", "--optimize-autoloader", "--with-all-dependencies", "--no-ansi", "--ignore-platform-reqs"}, packages...)
	for _, packageToKeep := range packagesToKeep {
		args = append(args, fmt.Sprintf("--with=%s", packageToKeep))
	}
	if minimalChanges {
		args = append(args, "--minimal-changes")
	}
	if dryRun {
		args = append(args, "--dry-run")
	} else {
		args = append(args, "--bump-after-update")
	}
	var changes []PackageChange
	out, err := s.execComposer(ctx, dir, args...)
	if err != nil {
		return changes, fmt.Errorf("failed to update dependencies: %w, output: %s, arg: %v", err, out, args)
	}

	// Regular expression to capture composer operations. The version character class includes
	// "+" so build-metadata versions (e.g. 1.0.0+21AF26D3) and "~" are matched too.
	const version = `[\w.\-+~]+`
	upgradeRegex := regexp.MustCompile(`- Upgrading ([\w\-/]+) \((` + version + `) => (` + version + `)\)`)
	downgradingRegex := regexp.MustCompile(`- Downgrading ([\w\-/]+) \((` + version + `) => (` + version + `)\)`)
	removeRegex := regexp.MustCompile(`- Removing ([\w\-/]+) \((` + version + `)\)`)
	installRegex := regexp.MustCompile(`- Installing ([\w\-/]+) \((` + version + `)\)`)

	// Match upgrades
	for _, match := range upgradeRegex.FindAllStringSubmatch(out, -1) {
		changes = append(changes, PackageChange{
			Action:  "Upgrade",
			Package: match[1],
			From:    match[2],
			To:      match[3],
		})
	}

	// Match downgrades
	for _, match := range downgradingRegex.FindAllStringSubmatch(out, -1) {
		changes = append(changes, PackageChange{
			Action:  "Downgrade",
			Package: match[1],
			From:    match[2],
			To:      match[3],
		})
	}

	// Match removals
	for _, match := range removeRegex.FindAllStringSubmatch(out, -1) {
		changes = append(changes, PackageChange{
			Action:  "Remove",
			Package: match[1],
			From:    match[2],
			To:      "",
		})
	}

	// Match installations
	for _, match := range installRegex.FindAllStringSubmatch(out, -1) {
		changes = append(changes, PackageChange{
			Action:  "Install",
			Package: match[1],
			From:    "",
			To:      match[2],
		})
	}

	return changes, nil
}

func (s *CLI) Install(ctx context.Context, dir string) error {
	out, err := s.execComposer(ctx, dir, "install", "--no-interaction", "--no-progress", "--optimize-autoloader", "--ignore-platform-reqs")
	if err != nil {
		return fmt.Errorf("failed to install dependencies: %w, output: %s", err, out)
	}
	return nil
}

func (s *CLI) Require(ctx context.Context, dir string, args ...string) (string, error) {
	out, err := s.execComposer(ctx, dir, append([]string{"require", "--ignore-platform-reqs"}, args...)...)
	if err != nil {
		return "", fmt.Errorf("failed to require package: %w, output: %s", err, out)
	}
	return out, nil
}

func (s *CLI) Remove(ctx context.Context, dir string, packages ...string) (string, error) {
	out, err := s.execComposer(ctx, dir, append([]string{"remove", "--ignore-platform-reqs"}, packages...)...)
	if err != nil {
		return "", fmt.Errorf("failed to remove package: %w, output: %s", err, out)
	}
	return out, nil
}

func (s *CLI) Audit(ctx context.Context, dir string) (Audit, error) {
	var composerAudit Audit
	out, err := s.execComposerJSON(ctx, dir, "audit", "--format=json", "--locked", "--no-plugins")
	if err != nil {
		// Some errors are expected for audit and don't affect the parsing
		s.logger.Debug("composer audit returned error", zap.Error(err))
	}

	if err := json.Unmarshal([]byte(out), &composerAudit); err != nil {
		return Audit{}, fmt.Errorf("failed to parse composer audit output: %w, output: %s", err, out)
	}

	return composerAudit, nil
}

// Source represents the source of an advisory.
type Source struct {
	Name     string `json:"name"`
	RemoteID string `json:"remoteId"`
}

// Advisory represents an individual security advisory.
type Advisory struct {
	ReportedAt       string   `json:"reportedAt"`
	Severity         string   `json:"severity"`
	AdvisoryID       string   `json:"advisoryId"`
	CVE              string   `json:"cve"`
	Sources          []Source `json:"sources"`
	Link             string   `json:"link"`
	PackageName      string   `json:"packageName"`
	AffectedVersions string   `json:"affectedVersions"`
	Title            string   `json:"title"`
}

// AdvisoriesMap represents the advisories mapping where keys are package names.
type AdvisoriesMap map[string]json.RawMessage

// Audit represents the flattened list of advisories.
type Audit struct {
	Advisories []Advisory `json:"advisories"`
}

// UnmarshalJSON flattens nested advisories into a single list.
func (c *Audit) UnmarshalJSON(data []byte) error {
	// Temporary map to parse nested structure
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	// Extract advisories field
	advisoriesData, exists := raw["advisories"]
	if !exists {
		return nil
	}

	// Flatten advisories
	var advisories []Advisory
	if advMap, ok := advisoriesData.(map[string]any); ok {
		for _, value := range advMap {
			switch v := value.(type) {
			case []any: // Simple advisory list
				for _, item := range v {
					var adv Advisory
					itemBytes, _ := json.Marshal(item)
					if err := json.Unmarshal(itemBytes, &adv); err != nil {
						return err
					}
					advisories = append(advisories, adv)
				}
			case map[string]any: // Nested map (e.g., drupal/core)
				for _, nestedItem := range v {
					var adv Advisory
					nestedBytes, _ := json.Marshal(nestedItem)
					if err := json.Unmarshal(nestedBytes, &adv); err != nil {
						return err
					}
					advisories = append(advisories, adv)
				}
			}
		}
	}

	c.Advisories = advisories
	return nil
}

// platformRequirementConstraint describes the unmet constraint for a failed/missing row of
// `composer check-platform-reqs --format=json` output.
type platformRequirementConstraint struct {
	Constraint string `json:"constraint"`
}

// platformRequirement is one row of `composer check-platform-reqs --format=json` output.
// Status is "success", "failed" (version constraint mismatch), or "missing" (package absent).
type platformRequirement struct {
	Name              string                         `json:"name"`
	Version           string                         `json:"version"`
	Status            string                         `json:"status"`
	FailedRequirement *platformRequirementConstraint `json:"failed_requirement"`
}

// CheckPlatformReqs verifies the PHP version satisfies the requirements in composer.lock.
// `Update` runs with --ignore-platform-reqs and no longer enforces this itself, so this is
// the only fail-fast check, run ahead of time for a clear message instead of a mid-update
// failure. Extension requirements (ext-*) are ignored: this tool doesn't
// control which extensions its own PHP runtime has loaded, so failing on those would block
// updates for a concern the operator can't act on here. A non-nil error means the PHP
// version requirement is unmet; the returned output names the offending requirement(s).
func (s *CLI) CheckPlatformReqs(ctx context.Context, dir string) (string, error) {
	out, err := s.execComposer(ctx, dir, "check-platform-reqs", "--lock", "--no-ansi", "--format=json")

	// composer prints an informational line ("Checking platform requirements using the
	// lock file") ahead of the JSON payload; combined stdout/stderr output can interleave
	// it before the array, so extract the array itself rather than parsing the whole blob.
	jsonPayload := out
	if start, end := strings.Index(out, "["), strings.LastIndex(out, "]"); start != -1 && end > start {
		jsonPayload = out[start : end+1]
	}

	var requirements []platformRequirement
	if jsonErr := json.Unmarshal([]byte(jsonPayload), &requirements); jsonErr != nil {
		if err != nil {
			return out, err
		}
		return out, fmt.Errorf("failed to parse composer check-platform-reqs output: %w, output: %s", jsonErr, out)
	}

	var failed []string
	for _, req := range requirements {
		if strings.HasPrefix(req.Name, "ext-") {
			continue
		}
		if req.Status == "success" {
			continue
		}
		required := "?"
		if req.FailedRequirement != nil {
			required = req.FailedRequirement.Constraint
		}
		failed = append(failed, fmt.Sprintf("%s: required %s, found %s (%s)", req.Name, required, req.Version, req.Status))
	}

	if len(failed) > 0 {
		return strings.Join(failed, "\n"), fmt.Errorf("unmet platform requirements")
	}

	return "", nil
}

func (s *CLI) Normalize(ctx context.Context, dir string) (string, error) {
	return s.execComposer(ctx, dir, "normalize")
}

func (s *CLI) Diff(ctx context.Context, dir string, withLinks bool) (string, error) {
	args := []string{"diff"}
	if withLinks {
		args = append(args, "--with-links")
	}

	out, err := s.execComposer(ctx, dir, args...)
	if err != nil {
		return "", err
	}

	if withLinks {
		// If the table is too long, GitHub/GitLab will not accept it. GitHub's and GitLab's MR
		// body limits (65536 and 1MB respectively) are byte limits, not rune counts, so measure
		// this the same way: a table full of multi-byte package/issue titles could pass a rune
		// count under the cap yet still exceed it in bytes. This is only the dependency-diff
		// section of the body, not the whole thing, so the threshold stays comfortably under
		// the smaller (GitHub) limit to leave room for the other addons' sections.
		if len(out) > 63000 {
			return s.Diff(ctx, dir, false)
		}
	}

	return out, err
}

func (s *CLI) GetInstalledPackageVersion(ctx context.Context, dir string, packageName string) (string, error) {
	out, err := s.execComposerJSON(ctx, dir, "show", packageName, "--locked", "--no-ansi", "--format=json")
	if err != nil {
		return "", err
	}

	var composerShow struct {
		Versions []string `json:"versions"`
	}

	if err := json.Unmarshal([]byte(out), &composerShow); err != nil {
		return "", err
	}

	if len(composerShow.Versions) == 0 {
		return "", fmt.Errorf("no versions found for package %s", packageName)
	}
	return composerShow.Versions[0], nil
}

// GetAllowPlugins returns composer's allow-plugins config as a package -> allowed map.
//
// The setting is polymorphic: composer accepts an object of per-package entries, the boolean
// `true` ("allow every plugin"), `false`, or nothing at all (reported as `[]`, `null`, or a
// non-zero exit). Only the object form carries per-package entries, so every other shape
// resolves to an empty map. The result is never nil — callers add newly discovered plugins to
// it, and writing to a nil map panics.
func (s *CLI) GetAllowPlugins(ctx context.Context, dir string) (map[string]bool, error) {
	allowPluginsJSON, err := s.GetConfig(ctx, dir, "allow-plugins")
	if err != nil {
		// `composer config allow-plugins` exits non-zero when the key is not set at all. That
		// is a legitimate project state, not a failure: there are simply no entries yet.
		s.logger.Debug("no composer allow-plugins config found", zap.Error(err))
		return map[string]bool{}, nil
	}

	allowPlugins := map[string]bool{}
	if err := json.Unmarshal([]byte(allowPluginsJSON), &allowPlugins); err != nil {
		switch strings.TrimSpace(allowPluginsJSON) {
		case "true", "false", "[]", "":
			return map[string]bool{}, nil
		}
		return nil, fmt.Errorf("failed to parse composer allow-plugins config %q: %w", allowPluginsJSON, err)
	}
	if allowPlugins == nil {
		// JSON null decodes successfully but sets the map to nil.
		return map[string]bool{}, nil
	}

	return allowPlugins, nil
}

func (s *CLI) SetAllowPlugins(ctx context.Context, dir string, plugins map[string]bool) error {
	for key, value := range plugins {
		err := s.SetConfig(ctx, dir, "allow-plugins."+key, fmt.Sprintf("%t", value))
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *CLI) GetConfig(ctx context.Context, dir string, key string) (string, error) {
	// Read from stdout only: the value is consumed as JSON/plain text and composer emits
	// unrelated warnings on stderr that would otherwise corrupt it.
	return s.execComposerJSON(ctx, dir, "config", "--json", key)
}

func (s *CLI) SetConfig(ctx context.Context, dir string, key string, value string) error {
	_, err := s.execComposer(ctx, dir, "config", "--json", key, value)
	return err
}

// GetDependencyPatches returns the patches declared by installed dependencies in
// composer.lock, as targetPackage -> set of patch files. composer-patches collects
// patches from every installed package, so these are applied in addition to the
// root composer.json patches.
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

// drupalOrgRepository is appended to every scratch project so a contrib module is resolvable even
// when the project under test declares its repositories somewhere this code cannot read.
const drupalOrgRepositoryURL = "https://packages.drupal.org/8"

// scratchComposerProject is the composer.json of the scratch project used to test whether a patch
// applies. It is written fresh before every check (see resetScratchProject) rather than once, so
// one check's `composer require` can never leave state a later, unrelated check reads.
type scratchComposerProject struct {
	Name         string            `json:"name"`
	Type         string            `json:"type"`
	Repositories []json.RawMessage `json:"repositories"`
	Require      map[string]string `json:"require"`
	Config       map[string]any    `json:"config"`
	Extra        map[string]any    `json:"extra"`
}

// buildScratchComposerJSON returns the scratch project's composer.json, carrying over the
// repositories declared by the project in projectDir.
//
// Carrying them over is what makes the check work for packages that are not on packagist.org or
// drupal.org: a private fork of a contrib module, an in-house module, or anything served only
// from a private registry such as Private Packagist. Without them composer cannot resolve the
// package here at all, `composer require` fails for a reason that has nothing to do with the
// patch, and — because that failure is read as "the patch does not apply" — composer_patches
// pins the package at its current version and tells the reviewer a patch conflicted when none
// did. That repeats on every run, so the package never updates again.
//
// The project's own repositories come first, keeping the priority they have in the project, so a
// private fork still wins over a public package of the same name. drupal.org is appended as a
// fallback and packagist.org is left enabled even when the project disables it: this project
// exists only to resolve one package plus composer-patches, and resolving more than the real
// project can costs nothing here and cannot affect the real project's lock.
func (s *CLI) buildScratchComposerJSON(projectDir string) ([]byte, error) {
	repositories := s.projectRepositories(projectDir)

	// Only add the drupal.org fallback when the project has not already declared it, so the
	// project's own entry (which may carry credentials or a mirror URL) is not shadowed.
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

// projectRepositories returns the repository entries declared by the project in projectDir, ready
// to be embedded in the scratch project.
//
// A project whose composer.json cannot be read or parsed yields no entries rather than an error:
// the scratch project still works for everything on packagist.org and drupal.org, and failing the
// patch check outright over it would be a worse outcome than checking against fewer repositories.
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

	// composer accepts both an array and an object keyed by name, and in both forms the order
	// entries appear in is the resolution priority — repositories are canonical by default, so
	// the first one offering a package name wins outright. That order has to survive into the
	// scratch project, or a private fork declared ahead of a public repository loses to it here
	// and its patch is tested against the wrong package.
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

// orderedObjectValues returns a JSON object's values in the order they are declared, and whether
// raw was an object at all.
//
// Decoding into a map[string]json.RawMessage would be shorter but loses that order, and sorting
// the keys to get a deterministic result — as this once did — reorders the caller's repositories
// into something the project never declared. Walking the token stream keeps the declared order,
// which is both the meaningful one and already deterministic.
func orderedObjectValues(raw json.RawMessage) ([]json.RawMessage, bool) {
	decoder := json.NewDecoder(bytes.NewReader(raw))

	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return nil, false
	}

	var values []json.RawMessage
	for decoder.More() {
		// The key, which the scratch project has no use for: composer identifies a repository by
		// its type and URL, and the name only ever matters for overriding an entry by key.
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

// normalizeRepository prepares one of the project's repository entries for the scratch project,
// reporting false for entries that must not be carried over.
//
// Two adjustments matter. A disable entry such as {"packagist.org": false} is dropped, because
// the scratch project needs packagist.org to resolve composer-patches even when the real project
// routes everything through a mirror. And a "path" repository's relative URL is resolved against
// the project, since the scratch project lives in a temp directory where that relative path
// points nowhere.
func normalizeRepository(raw json.RawMessage, projectDir string) (json.RawMessage, bool) {
	var entry map[string]any
	if err := json.Unmarshal(raw, &entry); err != nil {
		return nil, false
	}

	// The disable form is a single repository name mapped to false. Matched on that exact shape
	// rather than on "contains a boolean", because a real repository entry may legitimately
	// carry one — {"type":"composer","url":"...","canonical":false} is how a mirror is declared.
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

// resetScratchProject restores the scratch project to its pristine state before a new check
// runs. CheckIfPatchApplies and CheckIfPatchesApply both run `composer require <pkg>:<version>`
// in this directory, which permanently adds that package (at that exact version) to
// composer.json and composer.lock. Without resetting first, every later check inherits every
// earlier check's requirement: a version conflict between two unrelated packages checked earlier
// in the same run would make `composer require` fail for a reason that has nothing to do with
// whether the patch under test actually applies, and — because that failure is deliberately read
// as "the patch does not apply" — silently pins the wrong package version in the merge request.
//
// The composer.json is rebuilt here rather than once at init because it carries the repositories
// of the project under test, which are not known until a check asks for them.
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

// Cleanup removes the scratch composer project used for patch-apply checks. It is a no-op
// when no check ever ran, and safe to call more than once. Callers should defer it for the
// lifetime of the CLI: without it every run leaves a scratch project (with a full vendor
// tree) behind in the temp directory.
func (s *CLI) Cleanup() {
	if s.tempDir == "" {
		return
	}
	if err := s.fs.RemoveAll(s.tempDir); err != nil {
		s.logger.Warn("failed to remove composer scratch directory", zap.String("dir", s.tempDir), zap.Error(err))
	}
	s.tempDir = ""
	// Reset initOnce too: without this, a patch check running after Cleanup would see initOnce
	// already spent and skip straight past initTempDir with a stale, empty tempDir — writing
	// composer.patches.json and running `composer require` in the process's own working
	// directory (Dir: "" resolves to cwd) instead of a scratch project.
	s.initOnce = sync.Once{}
}

type patchTestConfig struct {
	Patches map[string]map[string]string `json:"patches"`
}

func (s *CLI) CheckIfPatchApplies(ctx context.Context, projectDir string, packageName string, packageVersion string, patchPath string) (bool, error) {

	s.initOnce.Do(s.initTempDir)
	if s.initErr != nil {
		return false, s.initErr
	}
	if err := s.resetScratchProject(projectDir); err != nil {
		return false, err
	}

	// Create a composer.patches.json file using json.Marshal to safely handle
	// special characters in packageName, packageVersion, and patchPath.
	patchConfig := patchTestConfig{
		Patches: map[string]map[string]string{
			packageName: {
				packageVersion: patchPath,
			},
		},
	}
	patchesJSONBytes, err := json.Marshal(patchConfig)
	if err != nil {
		return false, fmt.Errorf("failed to marshal patch config: %w", err)
	}
	patchesJSON := string(patchesJSONBytes)

	// Write the composer.patches.json file to the temporary directory
	if err := afero.WriteFile(s.fs, s.tempDir+"/composer.patches.json", []byte(patchesJSON), 0644); err != nil {
		return false, err
	}

	return s.requireIntoScratch(ctx, packageName, packageVersion)
}

// requireIntoScratch installs the package under test into the scratch project and classifies what
// happened: true when the patched package installed cleanly, false when composer obtained the
// package but the patch was rejected, and an error when composer could not obtain the package at
// all.
//
// That last case has to be told apart from the other two. A false return makes composer_patches
// pin the package at its current version and report a patch conflict in the merge request, which
// is the right answer for a patch that has gone stale and the wrong one for a package composer
// never saw — an unreachable registry, a rejected credential, or a package this scratch project's
// repositories do not carry. Reported as a conflict, that pins the package silently on every run
// with a reason the reviewer cannot act on; reported as an error, the callers leave the package
// alone and log it.
//
// Deliberately not --quiet: quiet suppresses the very message this classification reads. The
// output only ever reaches the debug log.
func (s *CLI) requireIntoScratch(ctx context.Context, packageName string, packageVersion string) (bool, error) {
	out, err := s.execComposer(ctx, s.tempDir,
		"require", "--ignore-platform-reqs", packageName+":"+packageVersion, "--with-all-dependencies")
	if err == nil {
		return true, nil
	}

	if reason, unresolvable := unresolvableReason(out); unresolvable {
		return false, fmt.Errorf("could not obtain %s %s to test its patches (%s): %w", packageName, packageVersion, reason, err)
	}

	return false, nil //nolint:nilerr // composer obtained the package and the patch was rejected
}

// unresolvableReason reports whether composer's output says it could not obtain the package at
// all, and why.
//
// A rejected patch is checked for first and wins outright, because the patterns below can appear
// in output that describes a run which failed for a completely different reason. Composer's
// dist-to-source fallback prints `The "<url>" file could not be downloaded (HTTP/1.1 404 Not
// Found)` and then carries on successfully; scanning for the unresolvable patterns alone would
// match that non-fatal warning and classify a genuine patch rejection as "could not obtain the
// package". The caller would then leave the package unpinned and omit it from the conflict
// report, and the merge request would ship a patch that does not apply — the inverse of the bug
// this classification exists to prevent, and a worse one.
//
// Both are matched on composer's human-readable English output, which is inherently brittle: a
// future Composer or composer-patches release that rewords a message silently changes the
// classification. Keep these patterns in step with the tools' actual wording, and prefer adding a
// pattern to loosening one.
func unresolvableReason(out string) (string, bool) {
	lower := strings.ToLower(out)

	// composer-patches v1 prints the first form on any patch failure, and the second when
	// composer-exit-on-patch-failure turns that failure into the exception that ends the run.
	if strings.Contains(lower, "could not apply patch") || strings.Contains(lower, "cannot apply patch") {
		return "", false
	}

	for _, candidate := range []struct{ pattern, reason string }{
		{"could not find package", "not available from any configured repository"},
		{"could not find a matching version", "not available from any configured repository"},
		{"could not be found", "not available from any configured repository"},
		{"invalid credentials", "repository authentication failed"},
		{"authentication required", "repository authentication failed"},
		{"could not be downloaded", "a required download failed"},
	} {
		if strings.Contains(lower, candidate.pattern) {
			return candidate.reason, true
		}
	}
	return "", false
}

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

func (s *CLI) GetInstalledPlugins(ctx context.Context, dir string) (map[string]any, error) {

	out, err := s.execComposer(ctx, dir, "depends", "composer-plugin-api", "--locked")
	if err != nil {
		return nil, err
	}

	// Match "<package> <version> requires ..." lines. The version token is matched loosely
	// (any non-space run) so pre-release and dev versions like 1.0.0-beta1 or dev-main are
	// captured, not just plain numeric versions.
	var packages = make(map[string]any)
	reg := regexp.MustCompile(`(?m)^(\S+)\s+\S+\s+requires\b`)
	matches := reg.FindAllStringSubmatch(out, -1)

	for _, match := range matches {
		if len(match) > 1 {
			packages[strings.TrimSpace(match[1])] = nil
		}
	}

	return packages, nil
}

func (s *CLI) IsPackageInstalled(ctx context.Context, dir string, packageToCheck string) (bool, error) {
	_, err := s.execComposer(ctx, dir, "show", "--locked", "--quiet", packageToCheck)
	if err != nil {
		return false, nil //nolint:nilerr // composer show failure means the package is not installed, not an error
	}
	return true, nil
}

func (s *CLI) GetLockHash(dir string) (string, error) {
	file, err := s.fs.Open(dir + "/composer.lock")
	if err != nil {
		return "", err
	}
	defer file.Close()

	var composerLock struct {
		ContentHash string `json:"content-hash"`
	}
	if err := json.NewDecoder(file).Decode(&composerLock); err != nil {
		return "", err
	}

	s.logger.Debug("composer lock hash", zap.String("hash", composerLock.ContentHash))

	return composerLock.ContentHash, nil
}

func (s *CLI) UpdateLockHash(ctx context.Context, dir string) error {
	_, err := s.execComposer(ctx, dir, "update", "--lock", "--no-install", "--ignore-platform-reqs")
	return err
}

func (s *CLI) GetCustomCodeDirectories(ctx context.Context, dir string) ([]string, error) {
	webroot, err := s.GetConfig(ctx, dir, "extra.drupal-scaffold.locations.web-root")
	if err != nil {
		s.logger.Error("failed to get Drupal web dir", zap.String("dir", dir), zap.Error(err))
		return nil, err
	}
	webroot = strings.TrimSuffix(webroot, "/")

	possibleDirectories := []string{webroot + "/modules/custom", webroot + "/themes/custom", webroot + "/profiles/custom"}
	var customCodeDirectories []string
	for _, possibleDirectory := range possibleDirectories {
		if _, err := s.fs.Stat(dir + "/" + possibleDirectory); os.IsNotExist(err) {
			continue
		}
		customCodeDirectories = append(customCodeDirectories, possibleDirectory)
	}
	return customCodeDirectories, nil
}
