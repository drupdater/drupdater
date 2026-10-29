package composer

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
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
	// extraEnv is added to every invocation; see WithoutBlocking.
	extraEnv []string

	tempDir  string
	initOnce sync.Once
	initErr  error
}

func NewCLI(logger *zap.Logger, opts ...Option) *CLI {
	s := &CLI{
		fs:     afero.NewOsFs(),
		logger: logger,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Option configures a CLI.
type Option func(*CLI)

// WithoutBlocking lets composer resolve to versions its policy blocks, such as ones with security
// advisories. A security run needs it: an advisory with no fixed version in range would otherwise
// make every update unresolvable, holding back the fixes that do exist.
func WithoutBlocking() Option {
	return func(s *CLI) {
		s.extraEnv = append(s.extraEnv, "COMPOSER_NO_BLOCKING=1")
	}
}

// requiredComposerEnv is forced on every invocation (see Env), not left to the Dockerfile, which
// only covers the published image. The 300s default timeout kills a large install mid-phase, and
// the implicit audit is noise in every update's log. Deployment policy is not forced here.
var requiredComposerEnv = []string{
	"COMPOSER_PROCESS_TIMEOUT=0",
	"COMPOSER_NO_AUDIT=1",
}

// Env applies requiredComposerEnv to env, passing everything else through in order — notably
// COMPOSER_AUTH. Inherited assignments to a required variable are dropped rather than shadowed,
// so the outcome does not depend on how os/exec resolves duplicate keys.
//
// Exported because drush, phpcs and rector run through `composer exec`; each should set
// command.Env = composer.Env(command.Environ()).
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

// command is built per call so a test swapping execCommand later still diverts the subprocess.
func (s *CLI) command(dir string) Command {
	return Command{New: execCommand, Logger: s.logger, Dir: dir, ExtraEnv: s.extraEnv}
}

func (s *CLI) execComposer(ctx context.Context, dir string, args ...string) (string, error) {
	return s.command(dir).Combined(ctx, args...)
}

// execComposerJSON returns stdout only, so composer's stderr notices cannot corrupt the payload.
func (s *CLI) execComposerJSON(ctx context.Context, dir string, args ...string) (string, error) {
	stdout, _, err := s.command(dir).Split(ctx, args...)
	return stdout, err
}

// Versions are the tool versions a run's outcome depends on.
type Versions struct {
	Composer string
	PHP      string
}

// composer --version puts its version on stdout and PHP's on stderr — hence the merged output.
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

// Update resolves into composer.json and composer.lock only; Install brings vendor/ to the result.
// A dry run leaves both files as it found them.
//
// The changes are read from a diff of composer.lock before and after, never from the console
// output, whose wording composer does not keep stable and which drops dev-branch commits.
func (s *CLI) Update(ctx context.Context, dir string, packages []string, packagesToKeep []string, minimalChanges bool, dryRun bool) (changes []PackageChange, err error) {
	lockPath := filepath.Join(dir, "composer.lock")
	baseLock, err := afero.ReadFile(s.fs, lockPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read composer.lock: %w", err)
	}

	// Lock only: vendor/ keeps the old code until Install, so sites can still be installed at it
	// after the update is known to change anything.
	args := append([]string{"update", "--no-interaction", "--no-progress", "--optimize-autoloader", "--with-all-dependencies", "--no-ansi", "--ignore-platform-reqs", "--no-install"}, packages...)
	for _, packageToKeep := range packagesToKeep {
		name, version, _ := strings.Cut(packageToKeep, ":")
		args = append(args, fmt.Sprintf("--with=%s:%s", name, constraint(version)))
	}
	if minimalChanges {
		args = append(args, "--minimal-changes")
	}
	if dryRun {
		// composer's own --dry-run writes no lock, leaving nothing to diff, so the files are put
		// back instead. No scripts: the project never asked for them on a result that is undone.
		args = append(args, "--no-scripts")
		restore, snapshotErr := s.snapshot(dir, "composer.json", "composer.lock")
		if snapshotErr != nil {
			return nil, snapshotErr
		}
		defer func() { err = errors.Join(err, restore()) }()
	} else {
		args = append(args, "--bump-after-update")
	}

	if out, err := s.execComposer(ctx, dir, args...); err != nil {
		return nil, fmt.Errorf("failed to update dependencies: %w, output: %s, arg: %v", err, out, args)
	}

	base, err := afero.TempFile(s.fs, "", "drupdater-base-*.lock")
	if err != nil {
		return nil, fmt.Errorf("failed to create the base lock file: %w", err)
	}
	defer func() {
		if err := s.fs.Remove(base.Name()); err != nil {
			s.logger.Warn("failed to remove the base lock file", zap.String("path", base.Name()), zap.Error(err))
		}
	}()
	_, writeErr := base.Write(baseLock)
	if err := errors.Join(writeErr, base.Close()); err != nil {
		return nil, fmt.Errorf("failed to write the base lock file: %w", err)
	}

	entries, err := s.diff(ctx, dir, "--base="+base.Name(), "--target=composer.lock")
	if err != nil {
		return nil, fmt.Errorf("failed to diff composer.lock: %w", err)
	}
	return packageChanges(entries)
}

// snapshot reads files under dir now and returns a func that writes them back as they were.
func (s *CLI) snapshot(dir string, files ...string) (func() error, error) {
	contents := make(map[string][]byte, len(files))
	for _, file := range files {
		path := filepath.Join(dir, file)
		content, err := afero.ReadFile(s.fs, path)
		if err != nil {
			return nil, fmt.Errorf("failed to read %s: %w", file, err)
		}
		contents[path] = content
	}
	return func() error {
		var errs []error
		for path, content := range contents {
			// Equivalent mutant: the file was just read, so it exists and the mode is never used.
			// mutator-disable-next-line numbers/incrementer,numbers/decrementer
			if err := afero.WriteFile(s.fs, path, content, 0644); err != nil {
				errs = append(errs, fmt.Errorf("failed to restore %s: %w", filepath.Base(path), err))
			}
		}
		return errors.Join(errs...)
	}, nil
}

// packageAction maps a composer-diff operation to a PackageChange action. A "change" is a new
// commit of the same version, which composer itself reports as an upgrade.
func packageAction(operation string) (string, bool) {
	switch operation {
	case "upgrade", "change":
		return "Upgrade", true
	case "downgrade":
		return "Downgrade", true
	case "remove":
		return "Remove", true
	case "install":
		return "Install", true
	}
	return "", false
}

// actionOrder is the order the changes come back in, which both the report and the MR carry.
var actionOrder = []string{"Upgrade", "Downgrade", "Remove", "Install"}

// packageChanges sorts by action, then package, so the result does not depend on how
// composer-diff split production from dev packages.
func packageChanges(entries []DiffEntry) ([]PackageChange, error) {
	var changes []PackageChange
	for _, entry := range entries {
		action, ok := packageAction(entry.Operation)
		if !ok {
			return nil, fmt.Errorf("unknown composer-diff operation %q for %s", entry.Operation, entry.Package)
		}
		changes = append(changes, PackageChange{Action: action, Package: entry.Package, From: entry.From, To: entry.To})
	}
	slices.SortFunc(changes, func(a, b PackageChange) int {
		return cmp.Or(
			cmp.Compare(slices.Index(actionOrder, a.Action), slices.Index(actionOrder, b.Action)),
			strings.Compare(a.Package, b.Package),
		)
	})
	return changes, nil
}

// constraint turns a version as composer prints it into one composer accepts back: a dev
// version prints as "1.x-dev 0424dff", but a constraint names its commit as "1.x-dev#0424dff".
func constraint(version string) string {
	if name, ref, ok := strings.Cut(version, " "); ok {
		return name + "#" + ref
	}
	return version
}

func (s *CLI) Install(ctx context.Context, dir string) error {
	out, err := s.execComposer(ctx, dir, "install", "--no-interaction", "--no-progress", "--optimize-autoloader", "--ignore-platform-reqs")
	if err != nil {
		return fmt.Errorf("failed to install dependencies: %w, output: %s", err, out)
	}
	return nil
}

// Audit returns both halves of `composer audit` output: advisories and abandoned packages.
// --abandoned is not passed, so the project's own `audit.abandoned: ignore` still applies.
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

// AbandonedPackage has an empty Replacement when composer reported JSON null — no successor.
type AbandonedPackage struct {
	PackageName string `json:"packageName"`
	Replacement string `json:"replacement"`
}

type Audit struct {
	Advisories []Advisory         `json:"advisories"`
	Abandoned  []AbandonedPackage `json:"abandoned"`
}

// UnmarshalJSON flattens nested advisories and the abandoned object into sorted lists.
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

// flattenAdvisories handles both shapes composer emits — a list, or a keyed map as drupal/core
// gets — since missing one silently drops an advisory from a security report.
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

	slices.SortFunc(advisories, compareAdvisories)

	return advisories, nil
}

// compareAdvisories orders the flattened list. Ranging a Go map is random, and this list reaches
// the run report, the merge request description and a security run's `composer update` arguments.
func compareAdvisories(a, b Advisory) int {
	return cmp.Or(
		strings.Compare(a.PackageName, b.PackageName),
		strings.Compare(a.AdvisoryID, b.AdvisoryID),
		strings.Compare(a.CVE, b.CVE),
		strings.Compare(a.Title, b.Title),
	)
}

// flattenAbandoned flattens composer audit's `abandoned` object into a sorted list. With nothing
// to report composer emits an empty JSON *array*, so a failed assertion must read as "none".
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

// platformRequirementConstraint is the unmet constraint of a failed or missing row.
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

// CheckPlatformReqs verifies the PHP version satisfies composer.lock, and is the only fail-fast
// check because `Update` runs with --ignore-platform-reqs. ext-* is ignored: the operator cannot
// act on which extensions drupdater's own runtime loaded.
func (s *CLI) CheckPlatformReqs(ctx context.Context, dir string) (string, error) {
	out, err := s.execComposer(ctx, dir, "check-platform-reqs", "--lock", "--no-ansi", "--format=json")

	// composer prints an informational line ahead of the payload, so extract just the array.
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

// DiffEntry is one package that differs between the committed composer.lock and the working one.
type DiffEntry struct {
	Package string
	// Operation is composer-diff's: install, upgrade, downgrade, remove or change, the last a new
	// commit of the same version.
	Operation string
	// From is empty for an install, To for a removal.
	From string
	To   string
	// Dev marks a packages-dev entry; Direct, one composer.json requires by name.
	Dev    bool
	Direct bool
	// CompareURL and ProjectURL are empty where composer-diff cannot derive one from the source.
	CompareURL string
	ProjectURL string
}

// Diff lists what changed in composer.lock since HEAD, production packages first, each by name.
func (s *CLI) Diff(ctx context.Context, dir string) ([]DiffEntry, error) {
	return s.diff(ctx, dir, "--with-links")
}

func (s *CLI) diff(ctx context.Context, dir string, args ...string) ([]DiffEntry, error) {
	out, err := s.execComposerJSON(ctx, dir, append([]string{"diff", "--format=json"}, args...)...)
	if err != nil {
		return nil, err
	}

	var raw struct {
		Packages    json.RawMessage `json:"packages"`
		PackagesDev json.RawMessage `json:"packages-dev"`
	}
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		return nil, fmt.Errorf("failed to parse composer diff output: %w", err)
	}

	prod, err := decodeDiffEntries(raw.Packages, false)
	if err != nil {
		return nil, err
	}
	dev, err := decodeDiffEntries(raw.PackagesDev, true)
	if err != nil {
		return nil, err
	}

	return append(prod, dev...), nil
}

// decodeDiffEntries accepts both shapes composer-diff emits: PHP encodes an empty list as [].
func decodeDiffEntries(data json.RawMessage, dev bool) ([]DiffEntry, error) {
	if len(data) == 0 || string(data) == "[]" {
		return nil, nil
	}

	var rows map[string]struct {
		Name          string `json:"name"`
		Direct        bool   `json:"direct"`
		Operation     string `json:"operation"`
		VersionBase   string `json:"version_base"`
		VersionTarget string `json:"version_target"`
		Compare       string `json:"compare"`
		Link          string `json:"link"`
	}
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil, fmt.Errorf("failed to parse composer diff output: %w", err)
	}

	entries := make([]DiffEntry, 0, len(rows))
	for _, row := range rows {
		entries = append(entries, DiffEntry{
			Package:    row.Name,
			Operation:  row.Operation,
			From:       row.VersionBase,
			To:         row.VersionTarget,
			Dev:        dev,
			Direct:     row.Direct,
			CompareURL: row.Compare,
			ProjectURL: row.Link,
		})
	}
	slices.SortFunc(entries, func(a, b DiffEntry) int { return strings.Compare(a.Package, b.Package) })

	return entries, nil
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

// AllowPlugins is composer.json's config.allow-plugins as the project wrote it.
type AllowPlugins struct {
	// Set is false when composer.json has no allow-plugins at all.
	Set bool
	// All is the value when it is a boolean, which decides every plugin at once.
	All *bool
	// Rules maps package patterns, in which * matches anything, to whether they may run.
	Rules map[string]bool
}

// Decides reports whether the setting already allows or denies plugin, matching patterns the way
// Composer's PluginManager does: anchored, case-insensitive, * for any characters.
func (a AllowPlugins) Decides(plugin string) bool {
	if a.All != nil {
		return true
	}
	for pattern := range a.Rules {
		re := "(?i)^" + strings.ReplaceAll(regexp.QuoteMeta(pattern), `\*`, ".*") + "$"
		if regexp.MustCompile(re).MatchString(plugin) {
			return true
		}
	}
	return false
}

// GetAllowPlugins reads composer.json itself: `composer config` prints {} both for an empty
// object and for no setting at all, and the two must be restored differently.
func (s *CLI) GetAllowPlugins(dir string) (AllowPlugins, error) {
	content, err := afero.ReadFile(s.fs, filepath.Join(dir, "composer.json"))
	if err != nil {
		return AllowPlugins{}, fmt.Errorf("failed to read composer.json: %w", err)
	}

	var manifest struct {
		Config struct {
			AllowPlugins json.RawMessage `json:"allow-plugins"`
		} `json:"config"`
	}
	if err := json.Unmarshal(content, &manifest); err != nil {
		return AllowPlugins{}, fmt.Errorf("failed to parse composer.json: %w", err)
	}

	raw := strings.TrimSpace(string(manifest.Config.AllowPlugins))
	switch raw {
	case "", "null":
		return AllowPlugins{}, nil
	case "true", "false":
		all := raw == "true"
		return AllowPlugins{Set: true, All: &all}, nil
	case "[]":
		return AllowPlugins{Set: true, Rules: map[string]bool{}}, nil
	}

	rules := map[string]bool{}
	if err := json.Unmarshal(manifest.Config.AllowPlugins, &rules); err != nil {
		return AllowPlugins{}, fmt.Errorf("failed to parse composer allow-plugins config %s: %w", raw, err)
	}
	return AllowPlugins{Set: true, Rules: rules}, nil
}

func (s *CLI) GetConfig(ctx context.Context, dir string, key string) (string, error) {
	// stdout only: composer's stderr warnings would corrupt the value.
	return s.execComposerJSON(ctx, dir, "config", "--json", key)
}

func (s *CLI) SetConfig(ctx context.Context, dir string, key string, value string) error {
	_, err := s.execComposer(ctx, dir, "config", "--json", key, value)
	return err
}

func (s *CLI) UnsetConfig(ctx context.Context, dir string, key string) error {
	_, err := s.execComposer(ctx, dir, "config", "--unset", key)
	return err
}

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

// dependsRegex matches a row of `composer depends`. The version token is any non-space run, so
// dev-main and 1.0.0-beta1 are captured too.
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

// ConfigGetter is the one method WebRoot needs, so a caller's own interface satisfies it.
type ConfigGetter interface {
	GetConfig(ctx context.Context, dir string, key string) (string, error)
}

// WebRoot returns the project's Drupal web root, relative to dir and without a trailing slash.
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
