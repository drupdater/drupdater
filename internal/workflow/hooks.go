package workflow

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/drupdater/drupdater/internal/report"
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
	run   func(E) (Result[any], error)
	// produces marks a hook built by Produce, whose finding the pipeline records.
	produces bool
}

// On builds the hook running fn at stage.
func On[E any](stage Stage, fn func(E) error) *Hook[E] {
	return &Hook[E]{Stage: stage, run: func(e E) (Result[any], error) { return Result[any]{}, fn(e) }}
}

// Produce builds the hook running fn at stage. The pipeline commits the Result's Commits in order
// and records its Finding for the addon's ReportData.
func Produce[E, F any](stage Stage, fn func(E) (Result[F], error)) *Hook[E] {
	return &Hook[E]{Stage: stage, produces: true, run: func(e E) (Result[any], error) {
		r, err := fn(e)
		return Result[any]{Commits: r.Commits, Finding: r.Finding}, err
	}}
}

// Observe builds an Inspect hook that only finds something and changes nothing.
func Observe[E, F any](fn func(E) (F, error)) *Hook[E] {
	return Produce(Inspect, func(e E) (Result[F], error) {
		finding, err := fn(e)
		return Result[F]{Finding: finding}, err
	})
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
	addons   []Addon
	hooks    []Hooks
	findings []*Findings
}

// NewPipeline reads each addon's hooks once.
func NewPipeline(addons []Addon) *Pipeline {
	p := &Pipeline{addons: addons, hooks: make([]Hooks, len(addons)), findings: make([]*Findings, len(addons))}
	for i, a := range addons {
		p.hooks[i] = a.Hooks()
		p.findings[i] = &Findings{}
	}
	return p
}

// Report adds every reporting addon's section to rec.
func (p *Pipeline) Report(rec *report.Recorder) {
	for i, a := range p.addons {
		if r, ok := a.(Reporter); ok {
			rec.AddSection(r.ReportKey(), r.ReportData(p.findings[i]))
			continue
		}
		rec.AddReporter(a)
	}
}

type staged interface {
	comparable
	stage() Stage
}

// event is one point in the run addons hook into: its name, which field of Hooks it reads, and
// the committer its hooks' Changes go to.
type event[H staged] interface {
	name() string
	hook(Hooks) H
	Commit(message string, paths ...string) (bool, error)
}

// Fire runs every addon's hook for e, by stage and within a stage in addon order.
func Fire[E event[*Hook[E]]](p *Pipeline, e E) error {
	site := ""
	if s, ok := any(e).(interface{ Site() string }); ok {
		site = s.Site()
	}
	for _, h := range ordered(p, e.hook) {
		result, err := h.hook.run(e)
		if err != nil {
			return fmt.Errorf("%s hook failed: %w", e.name(), err)
		}
		committed, err := commitAll(e, result.Commits)
		if err != nil {
			return fmt.Errorf("%s hook failed: %w", e.name(), err)
		}
		if h.hook.produces {
			p.findings[h.addon].Record(e.name(), site, result.Finding, committed)
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
		if i > 0 && h.hook.Stage != hooks[i-1].hook.Stage {
			settled = plan
		}
		e.plan = settled
		proposal, err := h.hook.Run(e)
		if err != nil {
			return UpdatePlan{}, fmt.Errorf("%s hook failed: %w", e.name(), err)
		}
		if plan, err = plan.merge(proposal); err != nil {
			// merge's errors carry no identity to unwrap, so %v here would read the same.
			// mutator-disable-next-line expression/errorf-wrap
			return UpdatePlan{}, fmt.Errorf("%s hooks disagree: %w", e.name(), err)
		}
	}
	return plan, nil
}

// addonHook is a hook with the index of the addon it belongs to.
type addonHook[H any] struct {
	addon int
	hook  H
}

// ordered sorts stably, so hooks sharing a stage keep addon order.
func ordered[H staged](p *Pipeline, pick func(Hooks) H) []addonHook[H] {
	var none H
	var hooks []addonHook[H]
	for i, addon := range p.hooks {
		if h := pick(addon); h != none {
			hooks = append(hooks, addonHook[H]{addon: i, hook: h})
		}
	}
	slices.SortStableFunc(hooks, func(a, b addonHook[H]) int { return cmp.Compare(a.hook.stage(), b.hook.stage()) })
	return hooks
}
