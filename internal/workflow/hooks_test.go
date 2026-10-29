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

	p := newPipeline(addons)
	require.NoError(t, fire("post-code-update", p.postCodeUpdate, &PostCodeUpdateEvent{}))

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

	p := newPipeline(addons)
	err := fire("pre-site-update", p.preSiteUpdate, &PreSiteUpdateEvent{})

	require.ErrorIs(t, err, assert.AnError)
	require.EqualError(t, err, "pre-site-update hook failed: "+assert.AnError.Error())
	assert.False(t, later, "a hook after the failing one must not run")
}

func TestPipelineSortsEveryEventsHooks(t *testing.T) {
	var ran []string
	record := func(name string) func() error {
		return func() error {
			ran = append(ran, name)
			return nil
		}
	}
	hooks := func(prefix string, stage Stage) Hooks {
		r := record(prefix)
		return Hooks{
			PreComposerUpdate:     On(stage, func(*PreComposerUpdateEvent) error { return r() }),
			PostComposerUpdate:    On(stage, func(*PostComposerUpdateEvent) error { return r() }),
			PostCodeUpdate:        On(stage, func(*PostCodeUpdateEvent) error { return r() }),
			PreSiteUpdate:         On(stage, func(*PreSiteUpdateEvent) error { return r() }),
			PostSiteUpdate:        On(stage, func(*PostSiteUpdateEvent) error { return r() }),
			PreMergeRequestCreate: On(stage, func(*PreMergeRequestCreateEvent) error { return r() }),
		}
	}
	p := newPipeline([]Addon{hookAddon{hooks("late", Inspect)}, hookAddon{hooks("early", Prepare)}})

	require.NoError(t, fire("", p.preComposerUpdate, &PreComposerUpdateEvent{}))
	require.NoError(t, fire("", p.postComposerUpdate, &PostComposerUpdateEvent{}))
	require.NoError(t, fire("", p.postCodeUpdate, &PostCodeUpdateEvent{}))
	require.NoError(t, fire("", p.preSiteUpdate, &PreSiteUpdateEvent{}))
	require.NoError(t, fire("", p.postSiteUpdate, &PostSiteUpdateEvent{}))
	require.NoError(t, fire("", p.preMergeRequestCreate, &PreMergeRequestCreateEvent{}))

	assert.Equal(t, []string{
		"early", "late", "early", "late", "early", "late",
		"early", "late", "early", "late", "early", "late",
	}, ran)
}
