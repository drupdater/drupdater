package workflow

import (
	"encoding/json"

	"github.com/drupdater/drupdater/internal/agent"
	"github.com/drupdater/drupdater/internal/drush"
)

// event is one point of a run the addons hook into, and what it hands their hooks.
type event struct {
	name string
	// site is the site of a per-site event, "" otherwise.
	site string
	// plan is the update as the run starts to settle it; pre-composer-update only.
	plan UpdatePlan
	// baseLock is composer.lock as committed when the event fired; post-composer-update only.
	baseLock json.RawMessage
	// facts are the site's facts as drush printed them; per-site events only.
	facts json.RawMessage
}

func preComposerUpdate(plan UpdatePlan) event {
	return event{name: EventPreComposerUpdate, plan: plan}
}

func postComposerUpdate(baseLock json.RawMessage) event {
	return event{name: EventPostComposerUpdate, baseLock: baseLock}
}

func postCodeUpdate() event {
	return event{name: EventPostCodeUpdate}
}

// preSiteUpdate carries the site's facts as they were before its update hooks ran.
func preSiteUpdate(site string, facts drush.Facts) event {
	return event{name: EventPreSiteUpdate, site: site, facts: facts.Raw}
}

// postSiteUpdate carries the site's facts read again after its update hooks ran: a hook can
// enable a module or move a path.
func postSiteUpdate(site string, facts drush.Facts) event {
	return event{name: EventPostSiteUpdate, site: site, facts: facts.Raw}
}

// payload is the event as a hook sees it, with the plan its stage settled.
func (e event) payload(settled UpdatePlan) agent.Event {
	payload := agent.Event{Name: e.name, Site: e.site, BaseLock: e.baseLock, Facts: e.facts}
	if e.name == EventPreComposerUpdate {
		payload.Plan = settled.toAgent()
	}
	return payload
}
