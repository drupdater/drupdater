package internal

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// defaultRegularAddons is the configurable addon set that runs in a normal update. Security
// mode defaults to none of these: it should be a minimal, focused security fix, with only the
// mandatory addons and the (automatically added) composer_audit running.
var defaultRegularAddons = []string{
	"code_beautifier",
	"deprecations_remover",
	"translations_updater",
	"composer_normalizer",
}

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
			Security: nil, // minimal by default; composer_audit is added automatically
		},
	}
}

// LoadConfigFile reads the .drupdater.yaml at path (layered over the built-in defaults) and
// applies sites, timeout, and addons onto c. A missing file is not an error: the defaults apply.
func LoadConfigFile(path string, c *Config) error {
	fc := defaultFileConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return applyFileConfig(fc, c)
		}
		return err
	}

	if err := yaml.Unmarshal(data, &fc); err != nil {
		return fmt.Errorf("parsing %s: %w", path, err)
	}

	if err := applyFileConfig(fc, c); err != nil {
		return fmt.Errorf("in %s: %w", path, err)
	}
	return nil
}

func applyFileConfig(fc fileConfig, c *Config) error {
	timeout, err := time.ParseDuration(fc.Timeout)
	if err != nil {
		return fmt.Errorf("invalid timeout %q: %w", fc.Timeout, err)
	}
	c.Sites = fc.Sites
	c.Timeout = timeout
	c.Addons = fc.Addons
	return nil
}
