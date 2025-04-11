package internal

type Config struct {
	ProjectID        string   `json:"projectId"`
	RepositoryURL    string   `json:"projectUrl"`
	Branch           string   `json:"projectBranch"`
	Token            string   `json:"projectToken"`
	Sites            []string `json:"projectSites"`
	PackagesToUpdate []string `json:"packagesToUpdate"`
	UpdateStrategy   string   `json:"updateStrategy"`
	AutoMerge        bool     `json:"autoMerge"`
	RunCBF           bool     `json:"runCBF"`
	RunRector        bool     `json:"runRector"`
	DryRun           bool     `json:"dryRun"`
	Verbose          bool     `json:"verbose"`
}
