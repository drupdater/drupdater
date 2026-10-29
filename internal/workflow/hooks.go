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

// Hooks is what an addon runs on each workflow event; a nil field means the addon sits it out.
type Hooks struct {
	PreComposerUpdate  *Hook[*PreComposerUpdateEvent]
	PostComposerUpdate *Hook[*PostComposerUpdateEvent]
	PostCodeUpdate     *Hook[*PostCodeUpdateEvent]
	PreSiteUpdate      *Hook[*PreSiteUpdateEvent]
	PostSiteUpdate     *Hook[*PostSiteUpdateEvent]
}

// Pipeline runs a set of addons' hooks, one method per event. Each method stops at the first
// failing hook, so a later one never sees a half-done event.
type Pipeline struct {
	preComposerUpdate  []*Hook[*PreComposerUpdateEvent]
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

func (p *Pipeline) PreComposerUpdate(e *PreComposerUpdateEvent) error {
	return run("pre-composer-update", p.preComposerUpdate, e)
}

func (p *Pipeline) PostComposerUpdate(e *PostComposerUpdateEvent) error {
	return run("post-composer-update", p.postComposerUpdate, e)
}

func (p *Pipeline) PostCodeUpdate(e *PostCodeUpdateEvent) error {
	return run("post-code-update", p.postCodeUpdate, e)
}

func (p *Pipeline) PreSiteUpdate(e *PreSiteUpdateEvent) error {
	return run("pre-site-update", p.preSiteUpdate, e)
}

func (p *Pipeline) PostSiteUpdate(e *PostSiteUpdateEvent) error {
	return run("post-site-update", p.postSiteUpdate, e)
}

func appendHook[E any](hooks []*Hook[E], h *Hook[E]) []*Hook[E] {
	if h == nil {
		return hooks
	}
	return append(hooks, h)
}

// sortByStage is stable, so hooks sharing a stage keep addon order.
func sortByStage[E any](hooks []*Hook[E]) {
	slices.SortStableFunc(hooks, func(a, b *Hook[E]) int { return cmp.Compare(a.Stage, b.Stage) })
}

func run[E any](event string, hooks []*Hook[E], e E) error {
	for _, h := range hooks {
		if err := h.Run(e); err != nil {
			return fmt.Errorf("%s hook failed: %w", event, err)
		}
	}
	return nil
}
