package workflow

import (
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
	PreComposerUpdate     *Hook[*PreComposerUpdateEvent]
	PostComposerUpdate    *Hook[*PostComposerUpdateEvent]
	PostCodeUpdate        *Hook[*PostCodeUpdateEvent]
	PreSiteUpdate         *Hook[*PreSiteUpdateEvent]
	PostSiteUpdate        *Hook[*PostSiteUpdateEvent]
	PreMergeRequestCreate *Hook[*PreMergeRequestCreateEvent]
}

// pipeline holds every addon's hooks, each event's already in run order.
type pipeline struct {
	preComposerUpdate     []func(*PreComposerUpdateEvent) error
	postComposerUpdate    []func(*PostComposerUpdateEvent) error
	postCodeUpdate        []func(*PostCodeUpdateEvent) error
	preSiteUpdate         []func(*PreSiteUpdateEvent) error
	postSiteUpdate        []func(*PostSiteUpdateEvent) error
	preMergeRequestCreate []func(*PreMergeRequestCreateEvent) error
}

func newPipeline(addons []Addon) *pipeline {
	all := make([]Hooks, 0, len(addons))
	for _, a := range addons {
		all = append(all, a.Hooks())
	}
	return &pipeline{
		preComposerUpdate:     ordered(all, func(h Hooks) *Hook[*PreComposerUpdateEvent] { return h.PreComposerUpdate }),
		postComposerUpdate:    ordered(all, func(h Hooks) *Hook[*PostComposerUpdateEvent] { return h.PostComposerUpdate }),
		postCodeUpdate:        ordered(all, func(h Hooks) *Hook[*PostCodeUpdateEvent] { return h.PostCodeUpdate }),
		preSiteUpdate:         ordered(all, func(h Hooks) *Hook[*PreSiteUpdateEvent] { return h.PreSiteUpdate }),
		postSiteUpdate:        ordered(all, func(h Hooks) *Hook[*PostSiteUpdateEvent] { return h.PostSiteUpdate }),
		preMergeRequestCreate: ordered(all, func(h Hooks) *Hook[*PreMergeRequestCreateEvent] { return h.PreMergeRequestCreate }),
	}
}

// ordered sorts stably, so hooks sharing a stage keep addon order.
func ordered[E any](all []Hooks, field func(Hooks) *Hook[E]) []func(E) error {
	var hooks []*Hook[E]
	for _, h := range all {
		if hook := field(h); hook != nil {
			hooks = append(hooks, hook)
		}
	}
	slices.SortStableFunc(hooks, func(a, b *Hook[E]) int { return int(a.Stage) - int(b.Stage) })

	runs := make([]func(E) error, 0, len(hooks))
	for _, h := range hooks {
		runs = append(runs, h.Run)
	}
	return runs
}

// fire stops at the first failing hook, so a later one never sees a half-done event.
func fire[E any](name string, hooks []func(E) error, e E) error {
	for _, run := range hooks {
		if err := run(e); err != nil {
			return fmt.Errorf("%s hook failed: %w", name, err)
		}
	}
	return nil
}
