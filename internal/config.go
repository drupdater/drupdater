package internal

type Config struct {
	RepositoryURL string
	Branch        string
	Token         string
	Sites         []string
	Security      bool
	SkipCBF       bool
	SkipRector    bool
	DryRun        bool
	Verbose       bool
}
