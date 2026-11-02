package workflow

// Addon is one addon of the agent's catalogue that a run runs, with the stage it hooks each event
// at. Its hooks and its report section live in the agent; the app orders and calls them.
type Addon struct {
	Name string
	// Hooks are the stage of each event the addon hooks, by event name, as the catalogue names it.
	Hooks map[string]string
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
