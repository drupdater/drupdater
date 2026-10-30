package composer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"sync"

	"github.com/spf13/afero"
	"go.uber.org/zap"
)

// scratchDir is the patch-test project's directory, created on first use and shared by every check.
type scratchDir struct {
	mu   sync.Mutex
	path string
}

func (d *scratchDir) get(fs afero.Fs) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.path != "" {
		return d.path, nil
	}
	path, err := afero.TempDir(fs, "", "composer-service")
	if err != nil {
		return "", err
	}
	d.path = path
	return path, nil
}

func (d *scratchDir) remove(fs afero.Fs) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.path == "" {
		return nil
	}
	err := fs.RemoveAll(d.path)
	d.path = ""
	return err
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

// resetScratchProject undoes the `composer require` each check leaves behind: without it, a
// conflict between two unrelated checks fails the require and reads as "the patch does not
// apply". composer.json is rebuilt here, not at init, because it carries the project's
// repositories, unknown until a check asks.
func (s *CLI) resetScratchProject(dir string, projectDir string) error {
	composerJSON, err := s.buildScratchComposerJSON(projectDir)
	if err != nil {
		return fmt.Errorf("failed to build scratch composer.json: %w", err)
	}
	if err := afero.WriteFile(s.fs, dir+"/composer.json", composerJSON, 0644); err != nil {
		return fmt.Errorf("failed to reset scratch composer.json: %w", err)
	}
	if err := s.fs.RemoveAll(dir + "/composer.lock"); err != nil {
		return fmt.Errorf("failed to remove scratch composer.lock: %w", err)
	}
	if err := s.fs.RemoveAll(dir + "/vendor"); err != nil {
		return fmt.Errorf("failed to remove scratch vendor directory: %w", err)
	}
	return nil
}

// Cleanup removes the patch-test scratch project; defer it, or every run leaks a vendor tree.
func (s *CLI) Cleanup() {
	if err := s.scratch.remove(s.fs); err != nil {
		s.logger.Warn("failed to remove composer scratch directory", zap.Error(err))
	}
}
