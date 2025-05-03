package composer

import (
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

type Runner interface {
	Update(ctx context.Context, dir string, packagesToUpdate []string, packagesToKeep []string, minimalChanges bool, dryRun bool) (string, error)
	Install(ctx context.Context, dir string) error
	Require(ctx context.Context, dir string, args ...string) (string, error)
	Remove(ctx context.Context, dir string, packages ...string) (string, error)
	Audit(ctx context.Context, dir string) (Audit, error)
	Normalize(ctx context.Context, dir string) (string, error)
	Diff(ctx context.Context, path string, targetBranch string, withLinks bool) (string, error)

	GetInstalledPackageVersion(ctx context.Context, dir string, packageName string) (string, error)
	GetAllowPlugins(ctx context.Context, dir string) (map[string]bool, error)
	SetAllowPlugins(ctx context.Context, dir string, plugins map[string]bool) error
	GetConfig(ctx context.Context, dir string, key string) (string, error)
	SetConfig(ctx context.Context, dir string, key string, value string) error

	ListPendingUpdates(ctx context.Context, dir string, packagesToUpdate []string, minimalChanges bool) ([]PackageChange, error)
	CheckIfPatchApplies(ctx context.Context, packageName string, packageVersion string, patchPath string) (bool, error)
	GetInstalledPlugins(ctx context.Context, dir string) (map[string]interface{}, error)
	IsPackageInstalled(ctx context.Context, dir string, packageToCheck string) (bool, error)
	GetLockHash(dir string) (string, error)
	UpdateLockHash(ctx context.Context, dir string) error
	GetCustomCodeDirectories(ctx context.Context, dir string) ([]string, error)
}

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
	s.logger.Sugar().Debugf("%s\n%s", command.String(), output)

	return output, err
}

func (s *CLI) Update(ctx context.Context, dir string, packages []string, packagesToKeep []string, minimalChanges bool, dryRun bool) (string, error) {
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
	out, err := s.execComposer(ctx, dir, args...)
	if err != nil {
		return "", fmt.Errorf("failed to update dependencies: %w, output: %s, arg: %v", err, out, args)
	}
	return out, nil
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
	out, err := s.execComposer(ctx, dir, "audit", "--format=json", "--locked", "--no-plugins")
	if err != nil {
		// Some errors are expected for audit and don't affect the parsing
		s.logger.Debug("composer audit returned error", zap.Error(err))
	}

	s.logger.Sugar().Info(out)

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
	var raw map[string]interface{}
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
	if advMap, ok := advisoriesData.(map[string]interface{}); ok {
		for _, value := range advMap {
			switch v := value.(type) {
			case []interface{}: // Simple advisory list
				for _, item := range v {
					var adv Advisory
					itemBytes, _ := json.Marshal(item)
					if err := json.Unmarshal(itemBytes, &adv); err != nil {
						return err
					}
					advisories = append(advisories, adv)
				}
			case map[string]interface{}: // Nested map (e.g., drupal/core)
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

func (s *CLI) Normalize(ctx context.Context, dir string) (string, error) {
	return s.execComposer(ctx, dir, "normalize")
}

func (s *CLI) Diff(ctx context.Context, dir string, targetBranch string, withLinks bool) (string, error) {
	args := []string{"diff", targetBranch}
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
			return s.Diff(ctx, dir, targetBranch, false)
		}
	}

	return out, err
}

func (s *CLI) GetInstalledPackageVersion(ctx context.Context, dir string, packageName string) (string, error) {
	out, err := s.execComposer(ctx, dir, "show", packageName, "--locked", "--no-ansi", "--format=json")
	if err != nil {
		return "", err
	}

	var composerShow struct {
		Versions []string `json:"versions"`
	}

	if err := json.Unmarshal([]byte(out), &composerShow); err != nil {
		return "", err
	}

	return composerShow.Versions[0], nil
}

func (s *CLI) GetAllowPlugins(ctx context.Context, dir string) (map[string]bool, error) {
	allowPluginsJSON, err := s.GetConfig(ctx, dir, "allow-plugins")
	if err != nil {
		s.logger.Error("failed to set composer config", zap.Error(err))
	}

	var allowPlugins map[string]bool

	err = json.Unmarshal([]byte(allowPluginsJSON), &allowPlugins)
	if err != nil {
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
	//ctx = context.Background()
	return s.execComposer(ctx, dir, "config", "--json", key)
}

func (s *CLI) SetConfig(ctx context.Context, dir string, key string, value string) error {
	_, err := s.execComposer(ctx, dir, "config", "--json", key, value)
	return err
}

// PackageChange represents an individual package operation
type PackageChange struct {
	Action  string // Install, Upgrade, Remove, Downgrade
	Package string
	From    string
	To      string
}

func (s *CLI) ListPendingUpdates(ctx context.Context, dir string, packagesToUpdate []string, minimalChanges bool) ([]PackageChange, error) {
	log, err := s.Update(ctx, dir, packagesToUpdate, []string{}, minimalChanges, true)
	if err != nil {
		return nil, err
	}

	var changes []PackageChange

	// Regular expression to capture upgrade operations
	upgradeRegex := regexp.MustCompile(`- Upgrading ([\w\-/]+) \(([\w\.\-]+) => ([\w\.\-]+)\)`)
	downgradingRegex := regexp.MustCompile(`- Downgrading ([\w\-/]+) \(([\w\.\-]+) => ([\w\.\-]+)\)`)
	removeRegex := regexp.MustCompile(`- Removing ([\w\-/]+) \(([\w\.\-]+)\)`)
	installRegex := regexp.MustCompile(`- Installing ([\w\-/]+) \(([\w\.\-]+)\)`)

	// Match upgrades
	for _, match := range upgradeRegex.FindAllStringSubmatch(log, -1) {
		changes = append(changes, PackageChange{
			Action:  "Upgrade",
			Package: match[1],
			From:    match[2],
			To:      match[3],
		})
	}

	// Match downgrades
	for _, match := range downgradingRegex.FindAllStringSubmatch(log, -1) {
		changes = append(changes, PackageChange{
			Action:  "Downgrade",
			Package: match[1],
			From:    match[2],
			To:      match[3],
		})
	}

	// Match removals
	for _, match := range removeRegex.FindAllStringSubmatch(log, -1) {
		changes = append(changes, PackageChange{
			Action:  "Remove",
			Package: match[1],
			From:    match[2],
			To:      "",
		})
	}

	// Match installations
	for _, match := range installRegex.FindAllStringSubmatch(log, -1) {
		changes = append(changes, PackageChange{
			Action:  "Install",
			Package: match[1],
			From:    "",
			To:      match[2],
		})
	}

	return changes, nil
}

func (s *CLI) initTempDir() {
	s.tempDir, s.initErr = afero.TempDir(s.fs, "", "composer-service")

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

func (s *CLI) CheckIfPatchApplies(ctx context.Context, packageName string, packageVersion string, patchPath string) (bool, error) {

	s.initOnce.Do(s.initTempDir)
	if s.initErr != nil {
		return false, s.initErr
	}

	// Create a composer.patches.json file
	patchesJSON := `{
	"patches": {
		"` + packageName + `": {
			"` + packageVersion + `": "` + patchPath + `"
		}
	}
}`

	// Write the composer.patches.json file to the temporary directory
	if err := afero.WriteFile(s.fs, s.tempDir+"/composer.patches.json", []byte(patchesJSON), 0644); err != nil {
		return false, err
	}

	// Run composer require in the temporary directory
	if _, err := s.Require(ctx, s.tempDir, packageName+":"+packageVersion, "--with-all-dependencies", "--quiet"); err != nil {
		return false, nil
	}

	return true, nil
}

func (s *CLI) GetInstalledPlugins(ctx context.Context, dir string) (map[string]interface{}, error) {

	out, err := s.execComposer(ctx, dir, "depends", "composer-plugin-api", "--locked")
	if err != nil {
		return nil, err
	}

	var packages = make(map[string]interface{})
	reg := regexp.MustCompile(`(?m)^(\S+)\s+v?[\d\.]+\s+requires`)
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
		return false, nil
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
