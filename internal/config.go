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
	RunTypes      RunTypesConfig
	// Concurrency bounds how many sites are installed/updated at once. It describes the
	// machine the run happens on, not the project, so it's a CLI flag rather than a
	// .drupdater.yaml key. A value <= 0 means "use GOMAXPROCS(0)".
	Concurrency int
	// ReportPath is where the machine-readable run report is written. Empty disables it. Like
	// Concurrency it describes this invocation rather than the project, so it is a CLI flag.
	ReportPath string
}

// RunTypesConfig groups the settings that differ between a normal and a security run. Keying
// on the run type rather than on the setting keeps everything one mode does in one block, so
// configuring a security run means reading one stanza instead of picking the `security` field
// out of every setting in the file.
type RunTypesConfig struct {
	Normal   RunTypeConfig `yaml:"normal"`
	Security RunTypeConfig `yaml:"security"`
}

// RunTypeConfig is what a single run type configures.
type RunTypeConfig struct {
	// Addons lists the configurable addons to run. Mandatory addons
	// (composer_allow_plugins, composer_patches, composer_diff, update_hooks, composer_audit,
	// unsupported_modules) always run and are not listed here.
	Addons []string `yaml:"addons"`

	// AutoMerge asks the platform to merge the MR/PR once its pipeline passes.
	AutoMerge bool `yaml:"auto_merge"`
}

// ActiveRunType returns the settings for the run type this invocation selected. Every consumer
// of a per-run-type setting goes through here, so the mapping from --security to a config block
// is stated once.
func (c Config) ActiveRunType() RunTypeConfig {
	if c.Security {
		return c.RunTypes.Security
	}
	return c.RunTypes.Normal
}
