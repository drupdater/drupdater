package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type hookAddon struct{ hooks Hooks }

func (a hookAddon) Hooks() Hooks                    { return a.hooks }
func (a hookAddon) RenderTemplate() (string, error) { return "", nil }

func failing[E any](E) error { return assert.AnError }

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

	require.NoError(t, NewPipeline(addons).PostCodeUpdate(&PostCodeUpdateEvent{}))

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

	err := NewPipeline(addons).PreSiteUpdate(&PreSiteUpdateEvent{})

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
			PreComposerUpdate:     On(stage, func(*PreComposerUpdateEvent) error { return r() }),
			PostComposerUpdate:    On(stage, func(*PostComposerUpdateEvent) error { return r() }),
			PostCodeUpdate:        On(stage, func(*PostCodeUpdateEvent) error { return r() }),
			PreSiteUpdate:         On(stage, func(*PreSiteUpdateEvent) error { return r() }),
			PostSiteUpdate:        On(stage, func(*PostSiteUpdateEvent) error { return r() }),
			PreMergeRequestCreate: On(stage, func(*PreMergeRequestCreateEvent) error { return r() }),
		}
	}
	p := NewPipeline([]Addon{hookAddon{hooks("late", Inspect)}, hookAddon{hooks("early", Prepare)}})
	broken := NewPipeline([]Addon{hookAddon{Hooks{
		PreComposerUpdate:     On(Change, failing[*PreComposerUpdateEvent]),
		PostComposerUpdate:    On(Change, failing[*PostComposerUpdateEvent]),
		PostCodeUpdate:        On(Change, failing[*PostCodeUpdateEvent]),
		PreSiteUpdate:         On(Change, failing[*PreSiteUpdateEvent]),
		PostSiteUpdate:        On(Change, failing[*PostSiteUpdateEvent]),
		PreMergeRequestCreate: On(Change, failing[*PreMergeRequestCreateEvent]),
	}}})

	fires := map[string]func(*Pipeline) error{
		"pre-composer-update":      func(p *Pipeline) error { return p.PreComposerUpdate(&PreComposerUpdateEvent{}) },
		"post-composer-update":     func(p *Pipeline) error { return p.PostComposerUpdate(&PostComposerUpdateEvent{}) },
		"post-code-update":         func(p *Pipeline) error { return p.PostCodeUpdate(&PostCodeUpdateEvent{}) },
		"pre-site-update":          func(p *Pipeline) error { return p.PreSiteUpdate(&PreSiteUpdateEvent{}) },
		"post-site-update":         func(p *Pipeline) error { return p.PostSiteUpdate(&PostSiteUpdateEvent{}) },
		"pre-merge-request-create": func(p *Pipeline) error { return p.PreMergeRequestCreate(&PreMergeRequestCreateEvent{}) },
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
