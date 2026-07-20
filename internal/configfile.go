package internal

import (
	"bytes"
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// defaultNormalAddons is the configurable addon set that runs in a normal update. Security
// mode defaults to none of these: it should be a minimal, focused security fix, with only the
// mandatory addons and the (automatically added) composer_audit running.
var defaultNormalAddons = []string{
	"code_beautifier",
	"deprecations_remover",
	"translations_updater",
	"composer_normalizer",
	"unsupported_modules",
}

// flexTimeout captures the raw scalar of the `timeout` key so both a quoted duration
// ("30m") and a bare number (0, which YAML decodes as an int) are accepted; the value is
// parsed as a Go duration later. Without this, `timeout: 0` — the documented way to disable
// the timeout — would fail to decode into a string field.
type flexTimeout string

func (t *flexTimeout) UnmarshalYAML(node *yaml.Node) error {
	*t = flexTimeout(node.Value)
	return nil
}

// fileConfig mirrors the YAML-settable keys of .drupdater.yaml. Timeout is captured as a raw
// scalar because yaml.v3 cannot decode a duration like "30m" into a time.Duration.
type fileConfig struct {
	Sites   []string     `yaml:"sites"`
	Timeout flexTimeout  `yaml:"timeout"`
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
			Normal:   defaultNormalAddons,
			Security: nil, // minimal by default; composer_audit is added automatically
		},
	}
}

// LoadConfigFile reads the .drupdater.yaml at path (layered over the built-in defaults) and
// applies sites, timeout, and addons onto c. A missing file is not an error: the defaults
// apply and found is false. Unknown keys in the file are rejected so typos fail loudly.
func LoadConfigFile(path string, c *Config) (found bool, err error) {
	fc := defaultFileConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, applyFileConfig(fc, c)
		}
		return false, err
	}

	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&fc); err != nil {
		return true, fmt.Errorf("parsing %s: %w", path, err)
	}

	if err := applyFileConfig(fc, c); err != nil {
		return true, fmt.Errorf("in %s: %w", path, err)
	}
	return true, nil
}

func applyFileConfig(fc fileConfig, c *Config) error {
	timeout, err := time.ParseDuration(string(fc.Timeout))
	if err != nil {
		return fmt.Errorf("invalid timeout %q (use a Go duration like \"30m\" or \"2h\", or 0 to disable): %w", string(fc.Timeout), err)
	}
	c.Sites = fc.Sites
	c.Timeout = timeout
	c.Addons = fc.Addons
	return nil
}
