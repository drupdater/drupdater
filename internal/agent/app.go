package agent

// The requests and replies of drupdater's PHP app (app/ in this repository), which speaks the
// same envelope as the agent. app/protocol holds an example of each.

// Author is whom the run's commits are by.
type Author struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// FireAddon is one addon a run runs: the stage it hooks each event at, and what its hooks found.
type FireAddon struct {
	Name string `json:"name"`
	// Hooks are the stage of each event the addon hooks, by event name.
	Hooks    map[string]string `json:"hooks"`
	Findings []Finding         `json:"findings"`
}

// FireRequest asks internal:fire to run every addon's hook of one event, in stage order,
// committing what each asks for before the next runs.
type FireRequest struct {
	Event  Event       `json:"event"`
	Addons []FireAddon `json:"addons"`
	Run    Run         `json:"run"`
	Author Author      `json:"author"`
}

// Recorded is a finding one addon's hook recorded.
type Recorded struct {
	Addon   string  `json:"addon"`
	Finding Finding `json:"finding"`
}

// FireReply is how firing an event ended, with what its hooks recorded before it ended.
type FireReply struct {
	Recorded []Recorded `json:"recorded"`
	// Plan is the merged plan; pre-composer-update only.
	Plan *Plan `json:"plan"`
	// Abort is why the run has nothing to do, when a hook found it has not.
	Abort string `json:"abort"`
	// Failure is why the event failed, when it did.
	Failure string `json:"failure"`
}

// CommitRequest asks internal:commit to commit exactly the paths each commit names.
type CommitRequest struct {
	Author  Author   `json:"author"`
	Commits []Commit `json:"commits"`
}

// CommitReply is the messages of the commits that were made: one whose paths hold no change is not.
type CommitReply struct {
	Committed []string `json:"committed"`
}
