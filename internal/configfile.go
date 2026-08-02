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
// to none of these — it should be a minimal, focused fix with only the mandatory addons.
//
// unsupported_modules is absent because it is mandatory now: end-of-life modules are worth
// knowing on a security run too, and it renders composer_audit's abandoned packages with them.
var defaultNormalAddons = []string{
	"code_beautifier",
	"deprecations_remover",
	"translations_updater",
	"composer_normalizer",
}

// flexTimeout captures the `timeout` key as a raw scalar so both "30m" and a bare 0 decode;
// it is parsed as a duration later. Without it `timeout: 0`, the documented way to disable the
// timeout, fails to decode into a string field.
type flexTimeout string

func (t *flexTimeout) UnmarshalYAML(node *yaml.Node) error {
	*t = flexTimeout(node.Value)
	return nil
}

// fileConfig mirrors the YAML-settable keys of .drupdater.yaml. Timeout is a raw scalar because
// yaml.v3 cannot decode "30m" into a time.Duration.
//
// Keys are split by scope: sites and timeout describe the whole run, anything that differs
// between a normal and a security update lives under run_types, where it cannot collide with a
// future global key.
type fileConfig struct {
	Sites    []string       `yaml:"sites"`
	Timeout  flexTimeout    `yaml:"timeout"`
	RunTypes RunTypesConfig `yaml:"run_types"`
}

// legacyProbe detects the pre-run_types layout, where addons and auto_merge were top-level keys
// split by mode. Strict decoding rejects those already, but with a message that says nothing
// about what to write instead.
type legacyProbe struct {
	Addons    *struct{} `yaml:"addons"`
	AutoMerge *struct{} `yaml:"auto_merge"`
}

// defaultFileConfig returns a fileConfig pre-populated with defaults. Unmarshaling over it
// overwrites only the keys present, so a partial file still resolves to a complete config.
func defaultFileConfig() fileConfig {
	return fileConfig{
		Sites:   []string{"default"},
		Timeout: "30m",
		RunTypes: RunTypesConfig{
			Normal: RunTypeConfig{Addons: defaultNormalAddons},
			// A security update should be a minimal, focused fix: mandatory addons only.
			Security: RunTypeConfig{},
		},
	}
}

// LoadConfigFile layers the .drupdater.yaml at path over the built-in defaults and applies the
// result to c. A missing file is not an error: defaults apply and found is false. Unknown keys
// are rejected so typos fail loudly.
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
	// An empty or comment-only file has no YAML document, which Decode reports as io.EOF. Same
	// intent as an absent file: no keys set, so the defaults apply.
	if err := dec.Decode(&fc); err != nil && !errors.Is(err, io.EOF) {
		return true, fmt.Errorf("parsing %s: %w", path, err)
	}

	if err := applyFileConfig(fc, c); err != nil {
		return true, fmt.Errorf("in %s: %w", path, err)
	}
	return true, nil
}

// checkLegacyLayout reports a config still in the pre-run_types layout, naming the replacement.
// The strict decode's own message is accurate but leaves the reader to guess the new shape.
func checkLegacyLayout(data []byte) error {
	var probe legacyProbe
	// Deliberately lenient: this only asks whether the legacy keys are present. A malformed
	// document is the strict decode's business, and it reports that with proper context.
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
	// Every per-site phase iterates this list, so an empty one silently skips all of them and
	// still opens a merge request for an update no site ever validated.
	if len(fc.Sites) == 0 {
		return errors.New(`no sites configured: "sites" must list at least one Drupal site name`)
	}
	c.Sites = fc.Sites
	c.Timeout = timeout
	c.RunTypes = fc.RunTypes
	return nil
}
