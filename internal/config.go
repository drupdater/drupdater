package internal

type Config struct {
	RepositoryURL  string   `json:"repositoryUrl"`
	Branch         string   `json:"branch"`
	Token          string   `json:"token"`
	Sites          []string `json:"sites"`
	UpdateStrategy string   `json:"updateStrategy"`
	AutoMerge      bool     `json:"autoMerge"`
	RunCBF         bool     `json:"runCBF"`
	RunRector      bool     `json:"runRector"`
	DryRun         bool     `json:"dryRun"`
	Verbose        bool     `json:"verbose"`
}
