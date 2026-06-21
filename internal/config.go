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
	// CommitStrategy is "bulk" (one commit for all packages, default) or "per_package"
	// (one atomic commit per direct package, including its config/patch/translation changes).
	CommitStrategy string
}

// AddonsConfig lists which configurable addons run in each mode. Mandatory addons
// (composer_allow_plugins, composer_patches, composer_diff, update_hooks) always run and are
// not listed here.
type AddonsConfig struct {
	Normal   []string `yaml:"normal"`
	Security []string `yaml:"security"`
}
