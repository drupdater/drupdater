package internal

import "time"

type Config struct {
	RepositoryURL string
	Branch        string
	Token         string
	WorkingDir    string
	Clone         bool
	Sites         []string
	Security      bool
	DryRun        bool
	Verbose       bool
	Timeout       time.Duration
	Addons        AddonsConfig
}

// AddonsConfig lists which configurable addons run in each mode. Mandatory addons
// (composer_allow_plugins, composer_patches, composer_diff, update_hooks) always run and are
// not listed here.
type AddonsConfig struct {
	Regular  []string `yaml:"regular"`
	Security []string `yaml:"security"`
}
