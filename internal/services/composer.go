package services

import (
	"encoding/json"
	"regexp"
	"strings"
	"sync"

	"ebersolve.com/updater/internal/utils"
	"github.com/spf13/afero"
	"go.uber.org/zap"
)

type ComposerService interface {
	GetComposerUpdates(dir string, packagesToUpdate []string, minimalChanges bool) ([]PackageChange, error)
	CheckPatchApplies(packageName string, packageVersion string, patchPath string) (bool, error)
	GetInstalledPlugins(dir string) (map[string]interface{}, error)
	RunComposerAudit(dir string) (ComposerAudit, error)
	GetComposerLockHash(dir string) (string, error)
}

func NewDefaultComposerService(logger *zap.Logger, commandExecutor utils.CommandExecutor) *DefaultComposerService {
	return &DefaultComposerService{
		fs:              afero.NewOsFs(),
		logger:          logger,
		commandExecutor: commandExecutor,
	}
}

type DefaultComposerService struct {
	fs              afero.Fs
	logger          *zap.Logger
	commandExecutor utils.CommandExecutor

	tempDir  string
	initOnce sync.Once
	initErr  error
}

// PackageChange represents an individual package operation
type PackageChange struct {
	Action  string // Install, Upgrade, Remove, Downgrade
	Package string
	From    string
	To      string
}

func (s *DefaultComposerService) GetComposerUpdates(dir string, packagesToUpdate []string, minimalChanges bool) ([]PackageChange, error) {
	s.logger.Debug("getting outdated packages")
	log, err := s.commandExecutor.UpdateDependencies(dir, packagesToUpdate, minimalChanges, true)
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

func (s *DefaultComposerService) initTempDir() {
	s.tempDir, s.initErr = afero.TempDir(s.fs, "", "composer-service")

	// Create a composer.json file
	composerJSON := `{
		"name": "ebersolve/patch-test",
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

func (s *DefaultComposerService) CheckPatchApplies(packageName string, packageVersion string, patchPath string) (bool, error) {

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
	if _, err := s.commandExecutor.InstallPackages(s.tempDir, packageName+":"+packageVersion, "--with-all-dependencies"); err != nil {
		return false, nil
	}

	return true, nil
}

func (s *DefaultComposerService) GetInstalledPlugins(dir string) (map[string]interface{}, error) {

	out, err := s.commandExecutor.ExecComposer(dir, "depends", "composer-plugin-api", "--locked")
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

// ComposerAudit represents the flattened list of advisories.
type ComposerAudit struct {
	Advisories []Advisory `json:"advisories"`
}

// UnmarshalJSON flattens nested advisories into a single list.
func (c *ComposerAudit) UnmarshalJSON(data []byte) error {
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

func (s *DefaultComposerService) RunComposerAudit(dir string) (ComposerAudit, error) {
	s.logger.Debug("running composer audit")

	var composerAudit ComposerAudit
	out, _ := s.commandExecutor.ExecComposer(dir, "audit", "--format=json", "--locked", "--no-plugins")

	if err := json.Unmarshal([]byte(out), &composerAudit); err != nil {
		return ComposerAudit{}, err
	}

	return composerAudit, nil
}

func (s *DefaultComposerService) GetComposerLockHash(dir string) (string, error) {
	s.logger.Debug("getting composer lock hash")
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
