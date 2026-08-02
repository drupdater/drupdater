package composer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
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

// requiredComposerEnv is forced on every invocation (see Env) rather than left to the
// Dockerfile, which only covers runs that use the published image.
//
//   - COMPOSER_PROCESS_TIMEOUT: composer's 300s default kills a large install mid-phase with a
//     message that names no timeout.
//   - COMPOSER_NO_AUDIT: the implicit post-update audit is output Update() cannot parse.
//     Auditing is the composer_audit addon's job.
//
// Deployment policy (COMPOSER_HOME, COMPOSER_CACHE_DIR, …) is deliberately not forced here.
var requiredComposerEnv = []string{
	"COMPOSER_PROCESS_TIMEOUT=0",
	"COMPOSER_NO_AUDIT=1",
}

// Env applies requiredComposerEnv to env, passing every other entry through in order — notably
// COMPOSER_AUTH, which carries registry credentials.
//
// Inherited assignments to a required variable are dropped rather than shadowed, so the outcome
// depends on this function and not on how os/exec resolves duplicate keys.
//
// Exported because drush, phpcs and rector run through `composer exec` and inherit the same
// process timeout. Every package building a composer *exec.Cmd should set:
//
//	command.Env = composer.Env(command.Environ())
func Env(env []string) []string {
	result := make([]string, 0, len(env)+len(requiredComposerEnv))
	for _, entry := range env {
		key, _, isAssignment := strings.Cut(entry, "=")
		// A non-assignment entry cannot override anything, so it is carried over.
		if isAssignment && slices.ContainsFunc(requiredComposerEnv, func(required string) bool {
			requiredKey, _, _ := strings.Cut(required, "=")
			return requiredKey == key
		}) {
			continue
		}
		result = append(result, entry)
	}
	return append(result, requiredComposerEnv...)
}

// command returns the runner for a composer invocation in dir. Built per call so a test that
// swaps execCommand after the CLI was constructed still diverts the subprocess.
func (s *CLI) command(dir string) Command {
	return Command{New: execCommand, Logger: s.logger, Dir: dir}
}

func (s *CLI) execComposer(ctx context.Context, dir string, args ...string) (string, error) {
	return s.command(dir).Combined(ctx, args...)
}

// execComposerJSON returns stdout only. Commands whose output is parsed as JSON must use this:
// composer's stderr notices would otherwise corrupt the payload. stderr still reaches the log.
func (s *CLI) execComposerJSON(ctx context.Context, dir string, args ...string) (string, error) {
	stdout, _, err := s.command(dir).Split(ctx, args...)
	return stdout, err
}

// Versions are the tool versions a run's outcome depends on: composer because drupdater wraps
// it, PHP because the image is built per PHP version.
type Versions struct {
	Composer string
	PHP      string
}

// composer --version prints its own version on stdout and PHP's on stderr, so both are scraped
// out of the merged output.
var (
	composerVersionRe = regexp.MustCompile(`(?m)^Composer version (\S+)`)
	phpVersionRe      = regexp.MustCompile(`(?m)^PHP version (\S+)`)
)

// Version reports the composer and PHP versions in play. Not tied to a project, hence no dir.
func (s *CLI) Version(ctx context.Context) (Versions, error) {
	out, err := s.command("").Combined(ctx, "--version", "--no-ansi")
	if err != nil {
		return Versions{}, fmt.Errorf("failed to determine composer version: %w, output: %s", err, out)
	}

	match := composerVersionRe.FindStringSubmatch(out)
	if match == nil {
		return Versions{}, fmt.Errorf("failed to parse composer version from output: %s", out)
	}
	versions := Versions{Composer: match[1]}

	// Composer only started reporting the PHP version in 2.5, so its absence is not an error.
	if phpMatch := phpVersionRe.FindStringSubmatch(out); phpMatch != nil {
		versions.PHP = phpMatch[1]
	}

	return versions, nil
}

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

	// Grouped by action rather than scanned line by line, so the result stays ordered by action
	// however composer interleaved its output.
	//
	// Deduplicated because composer reports a version change twice: once resolving the lock
	// ("Lock file operations") and again installing it ("Package operations"), in identical
	// wording. Installs differ between the two ("Locking" vs "Installing") and only the second is
	// matched, so nothing is lost by keeping the first of each. One update produces at most one
	// operation per package, so an exact repeat is always the same operation seen twice.
	seen := make(map[PackageChange]struct{})
	for _, pattern := range packageChangePatterns {
		for _, match := range pattern.re.FindAllStringSubmatch(out, -1) {
			change := PackageChange{Action: pattern.action, Package: match[1]}
			if pattern.from > 0 {
				change.From = match[pattern.from]
			}
			if pattern.to > 0 {
				change.To = match[pattern.to]
			}
			if _, ok := seen[change]; ok {
				continue
			}
			seen[change] = struct{}{}
			changes = append(changes, change)
		}
	}

	return changes, nil
}

// packageChangePatterns matches composer update's report of what it did, one entry per action.
//
// The order is the order the changes come back in, which the merge request description and the
// report both carry, so it is part of what a consumer sees.
var packageChangePatterns = []struct {
	action string
	re     *regexp.Regexp
	// from and to are the capture groups holding those versions; 0 means the action has none.
	// A removal reports only the version going away, an install only the one arriving.
	from int
	to   int
}{
	{action: "Upgrade", re: twoVersionRegex("Upgrading"), from: 2, to: 3},
	{action: "Downgrade", re: twoVersionRegex("Downgrading"), from: 2, to: 3},
	{action: "Remove", re: oneVersionRegex("Removing"), from: 2},
	{action: "Install", re: oneVersionRegex("Installing"), to: 2},
}

// versionPattern includes "+" and "~" in the class so build-metadata versions (1.0.0+21AF26D3)
// still match.
const versionPattern = `[\w.\-+~]+`

func twoVersionRegex(verb string) *regexp.Regexp {
	return regexp.MustCompile(`- ` + verb + ` ([\w\-/]+) \((` + versionPattern + `) => (` + versionPattern + `)\)`)
}

func oneVersionRegex(verb string) *regexp.Regexp {
	return regexp.MustCompile(`- ` + verb + ` ([\w\-/]+) \((` + versionPattern + `)\)`)
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

// Audit returns both halves of `composer audit` output: advisories and abandoned packages.
//
// --abandoned is deliberately not passed. The object is already in the JSON either way, and
// omitting the flag lets the project's own `audit.abandoned: ignore` keep dismissed packages
// out of every merge request.
func (s *CLI) Audit(ctx context.Context, dir string) (Audit, error) {
	var composerAudit Audit
	out, err := s.execComposerJSON(ctx, dir, "audit", "--format=json", "--locked", "--no-plugins")
	if err != nil {
		// audit exits non-zero when it finds advisories; the JSON is still valid.
		s.logger.Debug("composer audit returned error", zap.Error(err))
	}

	if err := json.Unmarshal([]byte(out), &composerAudit); err != nil {
		return Audit{}, fmt.Errorf("failed to parse composer audit output: %w, output: %s", err, out)
	}

	return composerAudit, nil
}

type Source struct {
	Name     string `json:"name"`
	RemoteID string `json:"remoteId"`
}

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

// AdvisoriesMap is keyed by package name.
type AdvisoriesMap map[string]json.RawMessage

// AbandonedPackage has an empty Replacement when the maintainers suggested no successor,
// which composer expresses as JSON null.
type AbandonedPackage struct {
	PackageName string `json:"packageName"`
	Replacement string `json:"replacement"`
}

type Audit struct {
	Advisories []Advisory         `json:"advisories"`
	Abandoned  []AbandonedPackage `json:"abandoned"`
}

// UnmarshalJSON flattens nested advisories into a single list and the abandoned object into a
// sorted list.
func (c *Audit) UnmarshalJSON(data []byte) error {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	advisories, err := flattenAdvisories(raw["advisories"])
	if err != nil {
		return err
	}

	c.Advisories = advisories
	c.Abandoned = flattenAbandoned(raw["abandoned"])
	return nil
}

// flattenAdvisories flattens composer audit's `advisories` object. Composer emits a package's
// advisories either as a list or as a keyed map (drupal/core does the latter); both shapes must
// land in the same list or an advisory silently vanishes from a security report. Any other
// shape means there is nothing to report.
func flattenAdvisories(advisoriesData any) ([]Advisory, error) {
	advMap, ok := advisoriesData.(map[string]any)
	// Equivalent mutant: ranging a nil map already yields (nil, nil). Kept to state the intent.
	// mutator-disable-next-line branch/if
	if !ok {
		return nil, nil
	}

	var advisories []Advisory
	for _, value := range advMap {
		var items []any
		switch v := value.(type) {
		case []any: // Simple advisory list
			items = v
		case map[string]any: // Nested map (e.g., drupal/core)
			items = slices.Collect(maps.Values(v))
		default:
			continue
		}

		for _, item := range items {
			var adv Advisory
			itemBytes, _ := json.Marshal(item)
			if err := json.Unmarshal(itemBytes, &adv); err != nil {
				return nil, err
			}
			advisories = append(advisories, adv)
		}
	}

	return advisories, nil
}

// flattenAbandoned flattens composer audit's `abandoned` object into a sorted list.
//
// With nothing to report composer emits an empty JSON *array*, not an object, so a failed
// assertion must read as "none" — this list is supplementary and no shape of it is worth
// failing a security run over. Sorted because map order is random and the report and merge
// request description must be byte-identical for unchanged input.
func flattenAbandoned(abandonedData any) []AbandonedPackage {
	entries, ok := abandonedData.(map[string]any)
	if !ok {
		return nil
	}

	abandoned := make([]AbandonedPackage, 0, len(entries))
	for name, replacement := range entries {
		pkg := AbandonedPackage{PackageName: name}
		// A non-string (null) means the maintainers named no successor.
		if suggestion, ok := replacement.(string); ok {
			pkg.Replacement = suggestion
		}
		abandoned = append(abandoned, pkg)
	}

	slices.SortFunc(abandoned, func(a, b AbandonedPackage) int {
		return strings.Compare(a.PackageName, b.PackageName)
	})

	return abandoned
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

// CheckPlatformReqs verifies the PHP version satisfies composer.lock. `Update` runs with
// --ignore-platform-reqs, so this is the only fail-fast check and runs ahead of it for a clear
// message. Extension requirements (ext-*) are ignored — the operator cannot act on which
// extensions drupdater's own runtime loaded. The returned output names the unmet requirements.
func (s *CLI) CheckPlatformReqs(ctx context.Context, dir string) (string, error) {
	out, err := s.execComposer(ctx, dir, "check-platform-reqs", "--lock", "--no-ansi", "--format=json")

	// composer prints an informational line ahead of the payload, so extract the array rather
	// than parsing the whole blob.
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
		// GitHub caps an MR body at 65536 *bytes*, so measure bytes, not runes. The margin
		// leaves room for the other addons' sections.
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
// The setting is polymorphic — an object of per-package entries, `true`, `false`, `[]`, `null`,
// or unset — and only the object form carries entries, so every other shape yields an empty
// map. Never nil: callers add newly discovered plugins to it.
func (s *CLI) GetAllowPlugins(ctx context.Context, dir string) (map[string]bool, error) {
	allowPluginsJSON, err := s.GetConfig(ctx, dir, "allow-plugins")
	if err != nil {
		// Composer exits non-zero when the key is unset — a legitimate state, not a failure.
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
	// stdout only: composer's stderr warnings would corrupt the value.
	return s.execComposerJSON(ctx, dir, "config", "--json", key)
}

func (s *CLI) SetConfig(ctx context.Context, dir string, key string, value string) error {
	_, err := s.execComposer(ctx, dir, "config", "--json", key, value)
	return err
}

// GetDependencyPatches returns patches declared by installed dependencies, as
// targetPackage -> set of patch files. These apply on top of the root composer.json patches.
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

// Appended to every scratch project so a contrib module resolves even when the project declares
// its repositories somewhere this code cannot read.
const drupalOrgRepositoryURL = "https://packages.drupal.org/8"

// scratchComposerProject is the composer.json of the patch-test project. Written fresh before
// every check (see resetScratchProject) so one check cannot leave state another reads.
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
// Without them a package from a private registry or in-house fork is unresolvable here, and
// composer_patches reads that failure as "the patch does not apply" — pinning the package and
// reporting a conflict that never happened, on every run.
//
// The project's repositories come first so a private fork keeps its priority. drupal.org is a
// fallback and packagist.org stays enabled even if the project disables it: resolving more than
// the real project can costs nothing and cannot affect its lock.
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

// projectRepositories returns the repository entries declared by the project in projectDir.
//
// An unreadable or unparsable composer.json yields no entries rather than an error: checking
// against fewer repositories beats failing the patch check outright.
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

// orderedObjectValues returns a JSON object's values in declaration order, and whether raw was
// an object at all. A map[string]json.RawMessage would be shorter but loses that order, and
// sorting the keys instead — as this once did — reorders the project's repositories.
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

// normalizeRepository prepares a repository entry for the scratch project, reporting false for
// entries that must not be carried over.
//
// A disable entry such as {"packagist.org": false} is dropped — the scratch project needs
// packagist.org for composer-patches even when the real project mirrors everything. A "path"
// repository's relative URL is resolved against the project, since the scratch project lives
// in a temp directory where that path points nowhere.
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

// resetScratchProject restores the scratch project before each check. Every check runs
// `composer require <pkg>:<version>` here, which sticks; without a reset a version conflict
// between two unrelated earlier checks fails the require, and that failure reads as "the patch
// does not apply" — silently pinning the wrong package.
//
// composer.json is rebuilt here rather than at init because it carries the project's
// repositories, which are unknown until a check asks.
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

// Cleanup removes the patch-test scratch project. A no-op when no check ran, and safe to call
// twice. Defer it for the lifetime of the CLI, or every run leaks a full vendor tree.
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

// CheckIfPatchApplies reports whether a single patch still applies to packageName at
// packageVersion.
func (s *CLI) CheckIfPatchApplies(ctx context.Context, projectDir string, packageName string, packageVersion string, patchPath string) (bool, error) {
	return s.CheckIfPatchesApply(ctx, projectDir, packageName, packageVersion, []string{patchPath})
}

// requireIntoScratch installs the package under test and classifies the outcome: true when the
// patched package installed, false when the patch was rejected, error when composer never got
// the package at all.
//
// The third case must stay distinct: false makes composer_patches pin the package and report a
// conflict, which is right for a stale patch and wrong for an unreachable registry or a rejected
// credential — that pins silently on every run for a reason the reviewer cannot act on.
//
// Deliberately not --quiet: that suppresses the message this classification reads.
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

// unresolvableReason reports whether composer's output says it could not obtain the package,
// and why.
//
// A rejected patch is checked first and wins outright: composer's dist-to-source fallback logs
// `… could not be downloaded (404)` and then succeeds, so matching the unresolvable patterns
// alone would misread a genuine patch rejection and ship a patch that does not apply.
//
// Both match composer's English output, so a reworded message silently changes the
// classification. Prefer adding a pattern to loosening one.
func unresolvableReason(out string) (string, bool) {
	lower := strings.ToLower(out)

	// composer-patches v1 prints the first form on any patch failure, and the second when
	// composer-exit-on-patch-failure turns that failure into the exception that ends the run.
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

// CheckIfPatchesApply reports whether patchPaths all apply together to packageName at
// packageVersion. The keys are zero-padded indexes: composer-patches v1 treats them as free-text
// descriptions, but applies them in key order, so the caller's order must survive JSON encoding.
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

// dependsRegex matches a row of `composer depends`. The version token matches any non-space
// run, so dev-main and 1.0.0-beta1 are captured too.
var dependsRegex = regexp.MustCompile(`(?m)^(\S+)\s+\S+\s+requires\b`)

func (s *CLI) GetInstalledPlugins(ctx context.Context, dir string) (map[string]any, error) {

	out, err := s.execComposer(ctx, dir, "depends", "composer-plugin-api", "--locked")
	if err != nil {
		return nil, err
	}

	var packages = make(map[string]any)
	matches := dependsRegex.FindAllStringSubmatch(out, -1)

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

// ConfigGetter is the one method WebRoot needs, so a caller's own narrow composer interface
// satisfies it without importing this package's whole CLI.
type ConfigGetter interface {
	GetConfig(ctx context.Context, dir string, key string) (string, error)
}

// WebRoot returns the project's Drupal web root, relative to dir and without a trailing slash.
// Everything Drupal — settings.php, custom modules, the config sync directory — is addressed
// from it, so several packages need the same lookup.
func WebRoot(ctx context.Context, cfg ConfigGetter, dir string) (string, error) {
	webroot, err := cfg.GetConfig(ctx, dir, "extra.drupal-scaffold.locations.web-root")
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(webroot, "/"), nil
}

func (s *CLI) GetCustomCodeDirectories(ctx context.Context, dir string) ([]string, error) {
	webroot, err := WebRoot(ctx, s, dir)
	if err != nil {
		s.logger.Error("failed to get Drupal web dir", zap.String("dir", dir), zap.Error(err))
		return nil, err
	}

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
