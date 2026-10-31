package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type hookAddon struct{ hooks Hooks }

func (a hookAddon) Hooks() Hooks { return a.hooks }

func failing[E any](E) error { return assert.AnError }

// propose builds a pre-composer-update hook that only proposes a plan.
func propose(stage Stage, fn func(*PreComposerUpdateEvent) (UpdatePlan, error)) *PlanHook {
	return ProduceProposal(stage, func(e *PreComposerUpdateEvent) (Proposal[struct{}], error) {
		plan, err := fn(e)
		return Proposal[struct{}]{Plan: plan}, err
	})
}

func TestHooksRunByStageThenAddonOrder(t *testing.T) {
	var ran []string
	hook := func(name string, stage Stage) Hooks {
		return Hooks{PostCodeUpdate: On(stage, func(*PostCodeUpdateEvent) error {
			ran = append(ran, name)
			return nil
		})}
	}
	addons := []Addon{
		hookAddon{hook("audit", Inspect)},
		hookAddon{hook("phpcbf", Format)},
		hookAddon{},
		hookAddon{hook("rector", Change)},
		hookAddon{hook("first", Prepare)},
		hookAddon{hook("second change", Change)},
	}

	require.NoError(t, Fire(NewPipeline(addons), &PostCodeUpdateEvent{}))

	assert.Equal(t, []string{"first", "rector", "second change", "phpcbf", "audit"}, ran)
}

func TestHooksStopAtTheFirstFailure(t *testing.T) {
	later := false
	addons := []Addon{
		hookAddon{Hooks{PreSiteUpdate: On(Change, failing[*PreSiteUpdateEvent])}},
		hookAddon{Hooks{PreSiteUpdate: On(Inspect, func(*PreSiteUpdateEvent) error {
			later = true
			return nil
		})}},
	}

	err := Fire(NewPipeline(addons), &PreSiteUpdateEvent{})

	require.ErrorIs(t, err, assert.AnError)
	require.EqualError(t, err, "pre-site-update hook failed: "+assert.AnError.Error())
	assert.False(t, later, "a hook after the failing one must not run")
}

// Every event's hooks are ordered, and every event's failure names that event.
func TestPipelineCoversEveryEvent(t *testing.T) {
	var ran []string
	record := func(name string) func() error {
		return func() error {
			ran = append(ran, name)
			return nil
		}
	}
	hooks := func(name string, stage Stage) Hooks {
		r := record(name)
		return Hooks{
			PreComposerUpdate:  propose(stage, func(*PreComposerUpdateEvent) (UpdatePlan, error) { return UpdatePlan{}, r() }),
			PostComposerUpdate: On(stage, func(*PostComposerUpdateEvent) error { return r() }),
			PostCodeUpdate:     On(stage, func(*PostCodeUpdateEvent) error { return r() }),
			PreSiteUpdate:      On(stage, func(*PreSiteUpdateEvent) error { return r() }),
			PostSiteUpdate:     On(stage, func(*PostSiteUpdateEvent) error { return r() }),
		}
	}
	p := NewPipeline([]Addon{hookAddon{hooks("late", Inspect)}, hookAddon{hooks("early", Prepare)}})
	broken := NewPipeline([]Addon{hookAddon{Hooks{
		PreComposerUpdate:  propose(Change, func(*PreComposerUpdateEvent) (UpdatePlan, error) { return UpdatePlan{}, assert.AnError }),
		PostComposerUpdate: On(Change, failing[*PostComposerUpdateEvent]),
		PostCodeUpdate:     On(Change, failing[*PostCodeUpdateEvent]),
		PreSiteUpdate:      On(Change, failing[*PreSiteUpdateEvent]),
		PostSiteUpdate:     On(Change, failing[*PostSiteUpdateEvent]),
	}}})

	fires := map[string]func(*Pipeline) error{
		"pre-composer-update": func(p *Pipeline) error {
			_, err := Settle(p, &PreComposerUpdateEvent{})
			return err
		},
		"post-composer-update": func(p *Pipeline) error { return Fire(p, &PostComposerUpdateEvent{}) },
		"post-code-update":     func(p *Pipeline) error { return Fire(p, &PostCodeUpdateEvent{}) },
		"pre-site-update":      func(p *Pipeline) error { return Fire(p, &PreSiteUpdateEvent{}) },
		"post-site-update":     func(p *Pipeline) error { return Fire(p, &PostSiteUpdateEvent{}) },
	}
	for event, fire := range fires {
		t.Run(event, func(t *testing.T) {
			ran = nil
			require.NoError(t, fire(p))
			assert.Equal(t, []string{"early", "late"}, ran)

			require.EqualError(t, fire(broken), event+" hook failed: "+assert.AnError.Error())
		})
	}
}

func proposing(stage Stage, proposal UpdatePlan, saw *UpdatePlan) Hooks {
	return Hooks{PreComposerUpdate: propose(stage, func(e *PreComposerUpdateEvent) (UpdatePlan, error) {
		if saw != nil {
			*saw = e.Plan()
		}
		return proposal, nil
	})}
}

func TestPreComposerUpdateMergesEveryProposal(t *testing.T) {
	addons := []Addon{
		hookAddon{proposing(Prepare, UpdatePlan{Only: []string{"drupal/core", "drupal/token"}, MinimalChanges: true}, nil)},
		hookAddon{proposing(Change, UpdatePlan{Keep: []Pin{pin("drupal/token", "1.13.0")}}, nil)},
		hookAddon{},
	}

	plan, err := Settle(NewPipeline(addons), NewPreComposerUpdateEvent(t.Context(), "/p", nil, UpdatePlan{}))

	require.NoError(t, err)
	assert.Equal(t, UpdatePlan{
		Only:           []string{"drupal/core", "drupal/token"},
		Keep:           []Pin{pin("drupal/token", "1.13.0")},
		MinimalChanges: true,
	}, plan)
}

func TestPreComposerUpdateStartsFromTheEventsPlan(t *testing.T) {
	var saw UpdatePlan
	start := UpdatePlan{Only: []string{"drupal/core"}}

	p := NewPipeline([]Addon{hookAddon{proposing(Change, UpdatePlan{}, &saw)}})
	plan, err := Settle(p, NewPreComposerUpdateEvent(t.Context(), "/p", nil, start))

	require.NoError(t, err)
	assert.Equal(t, start, saw)
	assert.Equal(t, start, plan)
}

func TestPreComposerUpdateHooksSeeOnlyEarlierStages(t *testing.T) {
	var firstChange, secondChange, inspect UpdatePlan
	narrow := UpdatePlan{Only: []string{"drupal/core"}}
	pinned := UpdatePlan{Keep: []Pin{pin("drupal/core", "10.2.0")}}
	addons := []Addon{
		hookAddon{proposing(Change, pinned, &firstChange)},
		hookAddon{proposing(Inspect, UpdatePlan{}, &inspect)},
		hookAddon{proposing(Prepare, narrow, nil)},
		hookAddon{proposing(Change, UpdatePlan{}, &secondChange)},
	}

	_, err := Settle(NewPipeline(addons), NewPreComposerUpdateEvent(t.Context(), "/p", nil, UpdatePlan{}))
	require.NoError(t, err)

	assert.Equal(t, narrow, firstChange, "Change sees what Prepare settled")
	assert.Equal(t, narrow, secondChange, "a stage-mate's proposal is not visible within the stage")
	assert.Equal(t, UpdatePlan{Only: narrow.Only, Keep: pinned.Keep}, inspect, "a later stage sees every earlier proposal")
}

func TestPreComposerUpdateResultIsIndependentOfAddonOrder(t *testing.T) {
	a := hookAddon{proposing(Change, UpdatePlan{Only: []string{"drupal/token", "drupal/core"}}, nil)}
	b := hookAddon{proposing(Change, UpdatePlan{Only: []string{"drupal/core"}, Keep: []Pin{pin("drupal/token", "1.0.0")}}, nil)}

	ab, err := Settle(NewPipeline([]Addon{a, b}), NewPreComposerUpdateEvent(t.Context(), "/p", nil, UpdatePlan{}))
	require.NoError(t, err)
	ba, err := Settle(NewPipeline([]Addon{b, a}), NewPreComposerUpdateEvent(t.Context(), "/p", nil, UpdatePlan{}))
	require.NoError(t, err)

	assert.Equal(t, ab, ba)
}

func TestPreComposerUpdateFailsWhenProposalsDisagree(t *testing.T) {
	addons := []Addon{
		hookAddon{proposing(Change, UpdatePlan{Keep: []Pin{pin("drupal/core", "10.2.0")}}, nil)},
		hookAddon{proposing(Change, UpdatePlan{Keep: []Pin{pin("drupal/core", "10.3.0")}}, nil)},
	}

	plan, err := Settle(NewPipeline(addons), NewPreComposerUpdateEvent(t.Context(), "/p", nil, UpdatePlan{}))

	require.EqualError(t, err, "pre-composer-update hooks disagree: drupal/core is pinned at both 10.2.0 and 10.3.0")
	assert.Zero(t, plan)
}

func TestPreComposerUpdateFailureReturnsNoPlan(t *testing.T) {
	addons := []Addon{
		hookAddon{proposing(Prepare, UpdatePlan{Only: []string{"drupal/core"}}, nil)},
		hookAddon{Hooks{PreComposerUpdate: propose(Change, func(*PreComposerUpdateEvent) (UpdatePlan, error) {
			return UpdatePlan{Only: []string{"drupal/core"}}, assert.AnError
		})}},
	}

	plan, err := Settle(NewPipeline(addons), NewPreComposerUpdateEvent(t.Context(), "/p", nil, UpdatePlan{}))

	require.ErrorIs(t, err, assert.AnError)
	assert.Zero(t, plan)
}

type describingAddon struct {
	hookAddon
	key, template string
}

func (a describingAddon) ReportKey() string           { return a.key }
func (describingAddon) ReportData(*Findings) any      { return nil }
func (a describingAddon) DescriptionTemplate() string { return a.template }

func TestDescriptionTemplatesAreThoseOfDescribingAddonsByReportKey(t *testing.T) {
	p := NewPipeline([]Addon{
		hookAddon{},
		describingAddon{key: "acme", template: "## Acme"},
		describingAddon{key: "other", template: "## Other"},
	})

	assert.Equal(t, map[string]string{"acme": "## Acme", "other": "## Other"}, p.DescriptionTemplates())
}
