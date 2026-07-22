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
	AutoMerge     AutoMergeConfig
	Verbose       bool
	Timeout       time.Duration
	Addons        AddonsConfig
}

// AddonsConfig lists which configurable addons run in each mode. Mandatory addons
// (composer_allow_plugins, composer_patches, composer_diff, update_hooks) always run and are
// not listed here.
type AddonsConfig struct {
	Normal   []string `yaml:"normal"`
	Security []string `yaml:"security"`
}

// AutoMergeConfig controls whether the MR/PR is set to auto-merge per update mode.
type AutoMergeConfig struct {
	Normal   bool `yaml:"normal"`
	Security bool `yaml:"security"`
}
