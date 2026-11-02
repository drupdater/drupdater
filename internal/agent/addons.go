package agent

import "encoding/json"

// Addon describes one addon of the agent's catalogue, as drupdater:addons lists it.
type Addon struct {
	Name string `json:"name"`
	// Mandatory addons run on every run; the others only where the run type configures them.
	Mandatory bool `json:"mandatory"`
	// Default addons are what a normal run gets when the project configures none.
	Default bool `json:"default"`
	// Hooks are the stage of each event the addon hooks, by event name.
	Hooks map[string]string `json:"hooks"`
}

// Plan is the update as the app reads and merges it.
type Plan struct {
	Only           []string `json:"only"`
	Keep           []Pin    `json:"keep"`
	MinimalChanges bool     `json:"minimal_changes"`
}

// Pin holds a package at a version as the lock prints it.
type Pin struct {
	Package string `json:"package"`
	Version string `json:"version"`
}

// Event is one workflow event as a hook sees it: its name and what that event carries.
type Event struct {
	Name string `json:"name"`
	Site string `json:"site,omitempty"`
	Plan *Plan  `json:"plan,omitempty"`
	// BaseLock is composer.lock as committed before the update; post-composer-update only.
	BaseLock json.RawMessage `json:"base_lock,omitempty"`
	// Facts are the site's facts as drush reported them; per-site events only.
	Facts json.RawMessage `json:"facts,omitempty"`
}

// Finding is what one hook of an addon found, with the messages of its commits that were made.
type Finding struct {
	Event     string          `json:"event"`
	Site      string          `json:"site"`
	Finding   json.RawMessage `json:"finding"`
	Committed []string        `json:"committed"`
}

// Run is what an addon needs to know about the run it is part of.
type Run struct {
	Security bool `json:"security"`
}

// Commit is one commit of exactly these paths.
type Commit struct {
	Message string   `json:"message"`
	Paths   []string `json:"paths"`
}

// AddonFindings is everything one addon's hooks found over a run.
type AddonFindings struct {
	Addon    string    `json:"addon"`
	Findings []Finding `json:"findings"`
}

// ReportRequest asks drupdater:report for the addons' sections of the run report.
type ReportRequest struct {
	Addons []AddonFindings `json:"addons"`
}

// ReportReply is the sections by addon name, leaving out the addons with nothing to report.
type ReportReply struct {
	Sections map[string]json.RawMessage `json:"sections"`
}

// MergeRequestRequest asks drupdater:merge-request to render the merge request from the report.
type MergeRequestRequest struct {
	// Report is the run report as published.
	Report json.RawMessage `json:"report"`
}

// MergeRequestReply is the merge request's title and description.
type MergeRequestReply struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}
