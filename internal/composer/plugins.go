package composer

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/afero"
)

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
