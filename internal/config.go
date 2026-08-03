package internal

import "time"

// Version is set at build time via -ldflags, and stays "dev" for builds that skip the Makefile.
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
	// Concurrency bounds how many sites run at once; <= 0 means GOMAXPROCS(0). A CLI flag, not
	// a config key: it describes the machine, not the project.
	Concurrency int
	// ReportPath is where the run report is written; empty disables it.
	ReportPath string
}

// RunTypesConfig is keyed on the run type, not the setting, so configuring one mode means
// reading one stanza rather than a `security` field in every setting.
type RunTypesConfig struct {
	Normal   RunTypeConfig `yaml:"normal"`
	Security RunTypeConfig `yaml:"security"`
}

// RunTypeConfig is what a single run type configures.
type RunTypeConfig struct {
	// Addons lists the configurable addons. The mandatory ones are not listed here.
	Addons []string `yaml:"addons"`

	// AutoMerge asks the platform to merge the MR/PR once its pipeline passes.
	AutoMerge bool `yaml:"auto_merge"`
}

// ActiveRunType is where --security maps to a config block — the only place that mapping lives.
func (c Config) ActiveRunType() RunTypeConfig {
	if c.Security {
		return c.RunTypes.Security
	}
	return c.RunTypes.Normal
}
