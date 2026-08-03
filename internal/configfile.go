package internal

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// defaultNormalAddons is the configurable addon set for a normal update. Security mode defaults
// to none of these: it should be a minimal, focused fix.
var defaultNormalAddons = []string{
	"code_beautifier",
	"deprecations_remover",
	"translations_updater",
	"composer_normalizer",
}

// flexTimeout takes `timeout` as a raw scalar so both "30m" and a bare 0 decode; a string field
// would reject `timeout: 0`, the documented way to disable it.
type flexTimeout string

func (t *flexTimeout) UnmarshalYAML(node *yaml.Node) error {
	*t = flexTimeout(node.Value)
	return nil
}

// fileConfig mirrors the YAML-settable keys of .drupdater.yaml. Split by scope: sites and timeout
// describe the whole run, per-mode settings live under run_types where they cannot collide.
type fileConfig struct {
	Sites    []string       `yaml:"sites"`
	Timeout  flexTimeout    `yaml:"timeout"`
	RunTypes RunTypesConfig `yaml:"run_types"`
}

// legacyProbe detects the pre-run_types layout. Strict decoding rejects it already, but says
// nothing about what to write instead.
type legacyProbe struct {
	Addons    *struct{} `yaml:"addons"`
	AutoMerge *struct{} `yaml:"auto_merge"`
}

// defaultFileConfig is the base to unmarshal over, so a partial file still resolves completely.
func defaultFileConfig() fileConfig {
	return fileConfig{
		Sites:   []string{"default"},
		Timeout: "30m",
		RunTypes: RunTypesConfig{
			Normal: RunTypeConfig{Addons: defaultNormalAddons},
			// Mandatory addons only, so a security update stays a focused fix.
			Security: RunTypeConfig{},
		},
	}
}

// LoadConfigFile layers path's .drupdater.yaml over the defaults and applies it to c. A missing
// file is not an error. Unknown keys are rejected so typos fail loudly.
func LoadConfigFile(path string, c *Config) (found bool, err error) {
	fc := defaultFileConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, applyFileConfig(fc, c)
		}
		return false, err
	}

	if err := checkLegacyLayout(data); err != nil {
		return true, fmt.Errorf("in %s: %w", path, err)
	}

	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	// A comment-only file has no YAML document, reported as io.EOF. Same intent as no file.
	if err := dec.Decode(&fc); err != nil && !errors.Is(err, io.EOF) {
		return true, fmt.Errorf("parsing %s: %w", path, err)
	}

	if err := applyFileConfig(fc, c); err != nil {
		return true, fmt.Errorf("in %s: %w", path, err)
	}
	return true, nil
}

// checkLegacyLayout reports a config still in the pre-run_types layout, naming the replacement.
func checkLegacyLayout(data []byte) error {
	var probe legacyProbe
	// Lenient on purpose: a malformed document is the strict decode's business to report.
	parsed := yaml.Unmarshal(data, &probe) == nil
	if !parsed || (probe.Addons == nil && probe.AutoMerge == nil) {
		return nil
	}
	return errors.New(`"addons" and "auto_merge" are now grouped per run type. Replace:
  addons:
    normal: [code_beautifier, ...]
    security: []
  auto_merge:
    normal: false
    security: true
with:
  run_types:
    normal:
      addons: [code_beautifier, ...]
      auto_merge: false
    security:
      addons: []
      auto_merge: true`)
}

func applyFileConfig(fc fileConfig, c *Config) error {
	timeout, err := time.ParseDuration(string(fc.Timeout))
	if err != nil {
		return fmt.Errorf("invalid timeout %q (use a Go duration like \"30m\" or \"2h\", or 0 to disable): %w", string(fc.Timeout), err)
	}
	// An empty list silently skips every per-site phase, then opens the merge request anyway.
	if len(fc.Sites) == 0 {
		return errors.New(`no sites configured: "sites" must list at least one Drupal site name`)
	}
	c.Sites = fc.Sites
	c.Timeout = timeout
	c.RunTypes = fc.RunTypes
	return nil
}
