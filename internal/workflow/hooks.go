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

// Pipeline runs a set of addons' hooks: Fire for most events, Settle for pre-composer-update.
// Either stops at the first failing hook, so a later one never sees a half-done event.
type Pipeline struct {
	addons []Hooks
}

// NewPipeline reads each addon's hooks once.
func NewPipeline(addons []Addon) *Pipeline {
	p := &Pipeline{addons: make([]Hooks, 0, len(addons))}
	for _, a := range addons {
		p.addons = append(p.addons, a.Hooks())
	}
	return p
}

type staged interface {
	comparable
	stage() Stage
}

// event is one point in the run addons hook into: its name, and which field of Hooks it reads.
type event[H staged] interface {
	name() string
	hook(Hooks) H
}

// Fire runs every addon's hook for e, by stage and within a stage in addon order.
func Fire[E event[*Hook[E]]](p *Pipeline, e E) error {
	for _, h := range ordered(p, e.hook) {
		if err := h.Run(e); err != nil {
			return fmt.Errorf("%s hook failed: %w", e.name(), err)
		}
	}
	return nil
}

// Settle returns the event's plan merged with every hook's proposal. Each hook sees the plan as
// earlier stages left it, so hooks sharing a stage cannot see each other's proposals.
func Settle(p *Pipeline, e *PreComposerUpdateEvent) (UpdatePlan, error) {
	hooks := ordered(p, e.hook)
	plan := e.plan.clone()
	settled := plan
	for i, h := range hooks {
		if i > 0 && h.Stage != hooks[i-1].Stage {
			settled = plan
		}
		e.plan = settled
		proposal, err := h.Run(e)
		if err != nil {
			return UpdatePlan{}, fmt.Errorf("%s hook failed: %w", e.name(), err)
		}
		if plan, err = plan.merge(proposal); err != nil {
			return UpdatePlan{}, fmt.Errorf("%s hooks disagree: %w", e.name(), err)
		}
	}
	return plan, nil
}

// ordered sorts stably, so hooks sharing a stage keep addon order.
func ordered[H staged](p *Pipeline, pick func(Hooks) H) []H {
	var none H
	var hooks []H
	for _, addon := range p.addons {
		if h := pick(addon); h != none {
			hooks = append(hooks, h)
		}
	}
	slices.SortStableFunc(hooks, func(a, b H) int { return cmp.Compare(a.stage(), b.stage()) })
	return hooks
}
