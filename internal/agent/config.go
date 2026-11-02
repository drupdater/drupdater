package agent

// ConfigRequest asks drupdater:config to read and check a project's .drupdater.yaml.
type ConfigRequest struct {
	// Path is absolute: the agent runs from a directory of its own.
	Path string `json:"path"`
}

// ConfigReply is the configuration a run uses, defaults applied.
type ConfigReply struct {
	// Found is false when there is no file, and the defaults are all there is.
	Found     bool     `json:"found"`
	Sites     []string `json:"sites"`
	TimeoutNS int64    `json:"timeout_ns"`
	RunTypes  struct {
		Normal   RunType `json:"normal"`
		Security RunType `json:"security"`
	} `json:"run_types"`
}

// RunType is what one run type does: the addons it runs, mandatory ones first, and auto-merge.
type RunType struct {
	Addons    []string `json:"addons"`
	AutoMerge bool     `json:"auto_merge"`
}
