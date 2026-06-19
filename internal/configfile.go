package internal

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Default addon lists (configurable addons only). These reproduce the historical behavior so
// an absent .drupdater.yaml changes nothing. Mandatory addons always run regardless.
var (
	defaultRegularAddons = []string{
		"code_beautifier",
		"deprecations_remover",
		"translations_updater",
		"composer_normalizer",
	}
	defaultSecurityAddons = []string{
		"composer_audit",
		"code_beautifier",
		"deprecations_remover",
		"translations_updater",
		"composer_normalizer",
	}
)

// fileConfig mirrors the YAML-settable keys of .drupdater.yaml. Timeout is a string because
// yaml.v3 cannot decode a duration like "30m" into a time.Duration.
type fileConfig struct {
	Sites   []string     `yaml:"sites"`
	Timeout string       `yaml:"timeout"`
	Addons  AddonsConfig `yaml:"addons"`
}

// defaultFileConfig returns a fileConfig pre-populated with defaults. Unmarshaling a YAML file
// over it only overwrites the keys actually present, so an absent or partial file still
// resolves to a complete config.
func defaultFileConfig() fileConfig {
	return fileConfig{
		Sites:   []string{"default"},
		Timeout: "30m",
		Addons: AddonsConfig{
			Regular:  defaultRegularAddons,
			Security: defaultSecurityAddons,
		},
	}
}

// LoadConfigFile reads the .drupdater.yaml at path, layering it over the built-in defaults. A
// missing file is not an error: the defaults are returned.
func LoadConfigFile(path string) (fileConfig, error) {
	fc := defaultFileConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fc, nil
		}
		return fc, err
	}

	if err := yaml.Unmarshal(data, &fc); err != nil {
		return fc, fmt.Errorf("parsing %s: %w", path, err)
	}

	if _, err := time.ParseDuration(fc.Timeout); err != nil {
		return fc, fmt.Errorf("invalid timeout %q in %s: %w", fc.Timeout, path, err)
	}

	return fc, nil
}

// Apply writes the file-derived values onto the Config. Timeout is already validated by
// LoadConfigFile, so the parse here cannot fail.
func (fc fileConfig) Apply(c *Config) {
	c.Sites = fc.Sites
	c.Timeout, _ = time.ParseDuration(fc.Timeout)
	c.Addons = fc.Addons
}
