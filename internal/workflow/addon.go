package workflow

import "fmt"

// Addon is one addon of the agent's catalogue that a run runs, with the stage it hooks each event
// at. Its hooks and its report section live in the agent; the pipeline only orders and calls them.
type Addon struct {
	Name string
	// Hooks are the stage of each event the addon hooks, by event name.
	Hooks map[string]Stage
}

// Stage orders the hooks of one event: every Prepare hook runs before any Change hook, and so on.
// Within a stage, hooks run in addon order.
type Stage int

const (
	// Prepare produces what later hooks of the same event read.
	Prepare Stage = iota
	// Change edits the project.
	Change
	// Format rewrites what Change wrote into the project's style.
	Format
	// Inspect reads the final state and changes nothing.
	Inspect
)

var stageNames = map[string]Stage{"prepare": Prepare, "change": Change, "format": Format, "inspect": Inspect}

// ParseStage reads a stage as the agent's catalogue names it.
func ParseStage(name string) (Stage, error) {
	stage, ok := stageNames[name]
	var err error
	if !ok {
		err = fmt.Errorf("unknown stage %q", name)
	}
	return stage, err
}

// The events addons hook into, in the order a run reaches them.
const (
	EventPreComposerUpdate  = "pre-composer-update"
	EventPostComposerUpdate = "post-composer-update"
	EventPostCodeUpdate     = "post-code-update"
	EventPreSiteUpdate      = "pre-site-update"
	EventPostSiteUpdate     = "post-site-update"
)

// Events lists every event an addon can hook.
var Events = []string{EventPreComposerUpdate, EventPostComposerUpdate, EventPostCodeUpdate, EventPreSiteUpdate, EventPostSiteUpdate}

// AbortError ends a run that has nothing to do. It is not a failure.
type AbortError struct {
	Msg string
}

func (e AbortError) Error() string {
	return e.Msg
}
