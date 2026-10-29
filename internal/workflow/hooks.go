package workflow

import (
	"cmp"
	"fmt"
	"slices"
)

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

// Hook is one addon's handler for the event E, run at Stage.
type Hook[E any] struct {
	Stage Stage
	Run   func(E) error
}

// On builds the hook running fn at stage.
func On[E any](stage Stage, fn func(E) error) *Hook[E] {
	return &Hook[E]{Stage: stage, Run: fn}
}

func (h *Hook[E]) stage() Stage { return h.Stage }

// PlanHook is a pre-composer-update hook: it reads the event's Plan and returns its proposal,
// which the zero UpdatePlan leaves unchanged.
type PlanHook struct {
	Stage Stage
	Run   func(*PreComposerUpdateEvent) (UpdatePlan, error)
}

// Propose builds the pre-composer-update hook running fn at stage.
func Propose(stage Stage, fn func(*PreComposerUpdateEvent) (UpdatePlan, error)) *PlanHook {
	return &PlanHook{Stage: stage, Run: fn}
}

func (h *PlanHook) stage() Stage { return h.Stage }

// Hooks is what an addon runs on each workflow event; a nil field means the addon sits it out.
type Hooks struct {
	PreComposerUpdate  *PlanHook
	PostComposerUpdate *Hook[*PostComposerUpdateEvent]
	PostCodeUpdate     *Hook[*PostCodeUpdateEvent]
	PreSiteUpdate      *Hook[*PreSiteUpdateEvent]
	PostSiteUpdate     *Hook[*PostSiteUpdateEvent]
}

// Pipeline runs a set of addons' hooks, one method per event. Each method stops at the first
// failing hook, so a later one never sees a half-done event.
type Pipeline struct {
	preComposerUpdate  []*PlanHook
	postComposerUpdate []*Hook[*PostComposerUpdateEvent]
	postCodeUpdate     []*Hook[*PostCodeUpdateEvent]
	preSiteUpdate      []*Hook[*PreSiteUpdateEvent]
	postSiteUpdate     []*Hook[*PostSiteUpdateEvent]
}

// NewPipeline reads each addon's hooks once.
func NewPipeline(addons []Addon) *Pipeline {
	p := &Pipeline{}
	for _, a := range addons {
		h := a.Hooks()
		p.preComposerUpdate = appendHook(p.preComposerUpdate, h.PreComposerUpdate)
		p.postComposerUpdate = appendHook(p.postComposerUpdate, h.PostComposerUpdate)
		p.postCodeUpdate = appendHook(p.postCodeUpdate, h.PostCodeUpdate)
		p.preSiteUpdate = appendHook(p.preSiteUpdate, h.PreSiteUpdate)
		p.postSiteUpdate = appendHook(p.postSiteUpdate, h.PostSiteUpdate)
	}
	sortByStage(p.preComposerUpdate)
	sortByStage(p.postComposerUpdate)
	sortByStage(p.postCodeUpdate)
	sortByStage(p.preSiteUpdate)
	sortByStage(p.postSiteUpdate)
	return p
}

// PreComposerUpdate returns the event's plan merged with every hook's proposal. Each hook sees
// the plan as earlier stages left it, so hooks sharing a stage cannot see each other's proposals.
func (p *Pipeline) PreComposerUpdate(e *PreComposerUpdateEvent) (UpdatePlan, error) {
	plan := e.plan.clone()
	settled := plan
	for i, h := range p.preComposerUpdate {
		if i > 0 && h.Stage != p.preComposerUpdate[i-1].Stage {
			settled = plan
		}
		e.plan = settled
		proposal, err := h.Run(e)
		if err != nil {
			return UpdatePlan{}, fmt.Errorf("pre-composer-update hook failed: %w", err)
		}
		if plan, err = plan.merge(proposal); err != nil {
			return UpdatePlan{}, fmt.Errorf("pre-composer-update hooks disagree: %w", err)
		}
	}
	return plan, nil
}

func (p *Pipeline) PostComposerUpdate(e *PostComposerUpdateEvent) error {
	return runHooks("post-composer-update", p.postComposerUpdate, e)
}

func (p *Pipeline) PostCodeUpdate(e *PostCodeUpdateEvent) error {
	return runHooks("post-code-update", p.postCodeUpdate, e)
}

func (p *Pipeline) PreSiteUpdate(e *PreSiteUpdateEvent) error {
	return runHooks("pre-site-update", p.preSiteUpdate, e)
}

func (p *Pipeline) PostSiteUpdate(e *PostSiteUpdateEvent) error {
	return runHooks("post-site-update", p.postSiteUpdate, e)
}

func appendHook[H comparable](hooks []H, h H) []H {
	var none H
	if h == none {
		return hooks
	}
	return append(hooks, h)
}

// sortByStage is stable, so hooks sharing a stage keep addon order.
func sortByStage[H interface{ stage() Stage }](hooks []H) {
	slices.SortStableFunc(hooks, func(a, b H) int { return cmp.Compare(a.stage(), b.stage()) })
}

func runHooks[E any](event string, hooks []*Hook[E], e E) error {
	for _, h := range hooks {
		if err := h.Run(e); err != nil {
			return fmt.Errorf("%s hook failed: %w", event, err)
		}
	}
	return nil
}
