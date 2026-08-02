package internal

import "time"

// Version is set at build time via -ldflags, and stays "dev" for builds that skip the Makefile.
// It is recorded in the run report so a consumer can tell which version produced a result.
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
	// Concurrency bounds how many sites are installed/updated at once; <= 0 means GOMAXPROCS(0).
	// A CLI flag, not a config key: it describes the machine, not the project.
	Concurrency int
	// ReportPath is where the run report is written; empty disables it. A CLI flag for the same
	// reason as Concurrency.
	ReportPath string
}

// RunTypesConfig groups the settings that differ between a normal and a security run. Keyed on
// the run type, not the setting, so configuring one mode means reading one stanza instead of
// picking a `security` field out of every setting in the file.
type RunTypesConfig struct {
	Normal   RunTypeConfig `yaml:"normal"`
	Security RunTypeConfig `yaml:"security"`
}

// RunTypeConfig is what a single run type configures.
type RunTypeConfig struct {
	// Addons lists the configurable addons to run. The mandatory ones always run and are not
	// listed here.
	Addons []string `yaml:"addons"`

	// AutoMerge asks the platform to merge the MR/PR once its pipeline passes.
	AutoMerge bool `yaml:"auto_merge"`
}

// ActiveRunType returns the selected run type's settings. Every consumer goes through here, so
// the mapping from --security to a config block is stated once.
func (c Config) ActiveRunType() RunTypeConfig {
	if c.Security {
		return c.RunTypes.Security
	}
	return c.RunTypes.Normal
}
