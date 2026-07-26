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
//
// Keys are split by scope: sites and timeout describe the whole run, everything that differs
// between a normal and a security update lives under run_types. Nesting the run types under
// their own key keeps them from ever colliding with a future global key.
type fileConfig struct {
	Sites    []string       `yaml:"sites"`
	Timeout  flexTimeout    `yaml:"timeout"`
	RunTypes RunTypesConfig `yaml:"run_types"`
}

// legacyProbe detects the pre-run_types layout, where addons and auto_merge were top-level
// keys each split by mode. Strict decoding already rejects them, but as "field addons not
// found in type internal.fileConfig" — which says nothing about what to write instead.
type legacyProbe struct {
	Addons    *struct{} `yaml:"addons"`
	AutoMerge *struct{} `yaml:"auto_merge"`
}

// defaultFileConfig returns a fileConfig pre-populated with defaults. Unmarshaling a YAML file
// over it only overwrites the keys actually present, so an absent or partial file still
// resolves to a complete config.
func defaultFileConfig() fileConfig {
	return fileConfig{
		Sites:   []string{"default"},
		Timeout: "30m",
		RunTypes: RunTypesConfig{
			Normal: RunTypeConfig{Addons: defaultNormalAddons},
			// Security defaults to no configurable addons: it should be a minimal, focused
			// fix, with only the mandatory ones and the automatic composer_audit running.
			Security: RunTypeConfig{},
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

	if err := checkLegacyLayout(data); err != nil {
		return true, fmt.Errorf("in %s: %w", path, err)
	}

	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	// A file that is empty or contains only comments has no YAML document, which Decode
	// reports as io.EOF. That is the same intent as an absent file — no keys are set — so it
	// resolves to the defaults rather than failing the run.
	if err := dec.Decode(&fc); err != nil && !errors.Is(err, io.EOF) {
		return true, fmt.Errorf("parsing %s: %w", path, err)
	}

	if err := applyFileConfig(fc, c); err != nil {
		return true, fmt.Errorf("in %s: %w", path, err)
	}
	return true, nil
}

// checkLegacyLayout reports a config still written in the pre-run_types layout, naming the
// replacement. Without it the run fails on the strict decode with "field addons not found in
// type internal.fileConfig", which is accurate but leaves the reader to guess the new shape.
func checkLegacyLayout(data []byte) error {
	var probe legacyProbe
	// Deliberately lenient: this asks only whether the legacy keys are present, so the decode
	// is consulted just when it parsed. A malformed document is not this function's business —
	// the strict decode in LoadConfigFile reports that, with proper context.
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
	// Reject an empty site list instead of running with it. Every per-site phase — installing
	// the baseline database, running update hooks, exporting configuration — iterates this
	// list, so an empty one would silently skip all of them and still open a merge request for
	// an update that was never validated against a site.
	if len(fc.Sites) == 0 {
		return errors.New(`no sites configured: "sites" must list at least one Drupal site name`)
	}
	c.Sites = fc.Sites
	c.Timeout = timeout
	c.RunTypes = fc.RunTypes
	return nil
}
