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
	// Concurrency bounds how many sites are installed/updated at once. It describes the
	// machine the run happens on, not the project, so it's a CLI flag rather than a
	// .drupdater.yaml key. A value <= 0 means "use GOMAXPROCS(0)".
	Concurrency int
}

// AddonsConfig lists which configurable addons run in each mode. Mandatory addons
// (composer_allow_plugins, composer_patches, composer_diff, update_hooks) always run and are
// not listed here.
type AddonsConfig struct {
	Normal   []string `yaml:"normal"`
	Security []string `yaml:"security"`
}
