package internal

import "time"

// Version is the drupdater version, set at build time via
// -ldflags "-X github.com/drupdater/drupdater/internal.Version=...". It defaults to "dev" for
// builds that do not set it (go run, go build without the Makefile). It is recorded in the run
// report so a consumer can tell which version produced a given result.
var Version = "dev"

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
	// ReportPath is where the machine-readable run report is written. Empty disables it. Like
	// Concurrency it describes this invocation rather than the project, so it is a CLI flag.
	ReportPath string
}

// AddonsConfig lists which configurable addons run in each mode. Mandatory addons
// (composer_allow_plugins, composer_patches, composer_diff, update_hooks) always run and are
// not listed here.
type AddonsConfig struct {
	Normal   []string `yaml:"normal"`
	Security []string `yaml:"security"`
}
