package composer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"

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
	args := append([]string{"update", "--no-interaction", "--no-progress", "--optimize-autoloader", "--with-all-dependencies", "--no-ansi"}, packages...)
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
	out, err := s.execComposer(ctx, dir, "install", "--no-interaction", "--no-progress", "--optimize-autoloader")
	if err != nil {
		return fmt.Errorf("failed to install dependencies: %w, output: %s", err, out)
	}
	return err
}

func (s *CLI) Require(ctx context.Context, dir string, args ...string) (string, error) {
	out, err := s.execComposer(ctx, dir, append([]string{"require"}, args...)...)
	if err != nil {
		return "", fmt.Errorf("failed to require package: %w, output: %s", err, out)
	}
	return out, nil
}

func (s *CLI) Remove(ctx context.Context, dir string, packages ...string) (string, error) {
	out, err := s.execComposer(ctx, dir, append([]string{"remove"}, packages...)...)
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

// CheckPlatformReqs verifies the platform (PHP version and extensions) satisfies the
// requirements in composer.lock. `composer update` enforces the same checks, so this lets
// us fail fast with a clear message instead of mid-update. A non-nil error means a
// requirement is unmet; the returned output names the offending requirement(s).
func (s *CLI) CheckPlatformReqs(ctx context.Context, dir string) (string, error) {
	return s.execComposer(ctx, dir, "check-platform-reqs", "--lock", "--no-ansi")
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
		// If table is too long, Github/Gitlab will not accept it. So we use the version without the links.
		tableCharCount := utf8.RuneCountInString(out)
		if tableCharCount > 63000 {
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

func (s *CLI) GetAllowPlugins(ctx context.Context, dir string) (map[string]bool, error) {
	allowPluginsJSON, err := s.GetConfig(ctx, dir, "allow-plugins")
	if err != nil {
		return nil, fmt.Errorf("failed to get composer allow-plugins config: %w", err)
	}

	var allowPlugins map[string]bool
	if err := json.Unmarshal([]byte(allowPluginsJSON), &allowPlugins); err != nil {
		return nil, err
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

func (s *CLI) initTempDir() {
	s.tempDir, s.initErr = afero.TempDir(s.fs, "", "composer-service")
	if s.initErr != nil {
		return
	}

	// Create a composer.json file
	composerJSON := `{
		"name": "drupdater/patch-test",
		"type": "project",
		"repositories": [
			{
				"type": "composer",
				"url": "https://packages.drupal.org/8"
			}
		],
		"require": {
			"cweagans/composer-patches": "~1.0"
		},
		"config": {
			"allow-plugins": true
		},
		"extra": {
			"composer-exit-on-patch-failure": true,
			"patches-file": "composer.patches.json"
		}
	}`

	// Write the composer.json file to the temporary directory
	s.initErr = afero.WriteFile(s.fs, s.tempDir+"/composer.json", []byte(composerJSON), 0644)
}

type patchTestConfig struct {
	Patches map[string]map[string]string `json:"patches"`
}

func (s *CLI) CheckIfPatchApplies(ctx context.Context, packageName string, packageVersion string, patchPath string) (bool, error) {

	s.initOnce.Do(s.initTempDir)
	if s.initErr != nil {
		return false, s.initErr
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

	// Run composer require in the temporary directory
	if _, err := s.Require(ctx, s.tempDir, packageName+":"+packageVersion, "--with-all-dependencies", "--quiet"); err != nil {
		return false, nil //nolint:nilerr // composer failure means the patch does not apply, not an error
	}

	return true, nil
}

func (s *CLI) CheckIfPatchesApply(ctx context.Context, packageName string, packageVersion string, patchPaths []string) (bool, error) {
	s.initOnce.Do(s.initTempDir)
	if s.initErr != nil {
		return false, s.initErr
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

	if _, err := s.Require(ctx, s.tempDir, packageName+":"+packageVersion, "--with-all-dependencies", "--quiet"); err != nil {
		return false, nil //nolint:nilerr // composer failure means the patches do not apply, not an error
	}
	return true, nil
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
	_, err := s.execComposer(ctx, dir, "update", "--lock", "--no-install")
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
