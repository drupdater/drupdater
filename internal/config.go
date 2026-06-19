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
	SkipCBF       bool
	SkipRector    bool
	DryRun        bool
	Verbose       bool
	Timeout       time.Duration
}
