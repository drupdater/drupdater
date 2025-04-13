package internal

type Config struct {
	RepositoryURL  string
	Branch         string
	Token          string
	Sites          []string
	UpdateStrategy string
	AutoMerge      bool
	SkipCBF        bool
	SkipRector     bool
	DryRun         bool
	Verbose        bool
}
