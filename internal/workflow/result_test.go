package workflow

import (
	"strconv"
	"sync"
	"testing"

	"github.com/drupdater/drupdater/internal/drush"
	"github.com/drupdater/drupdater/internal/report"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func siteEvent(t *testing.T, committer Committer) *PostSiteUpdateEvent {
	t.Helper()
	return NewPostSiteUpdateEvent(t.Context(), "/project", committer, "default", drush.Facts{})
}

func TestProducedCommitsAreMadeInOrderAndRecordedWhenMade(t *testing.T) {
	committer := NewMockCommitter(t)
	committer.EXPECT().Commit("Add PHPCS config", []string{"phpcs.xml"}).Return(true, nil).Once()
	committer.EXPECT().Commit("Update coding styles", []string{"web/a.php"}).Return(false, nil).Once()
	p := NewPipeline([]Addon{hookAddon{Hooks{PostSiteUpdate: Produce(Format, func(*PostSiteUpdateEvent) (Result[string], error) {
		return Result[string]{
			Commits: []Commit{{"Add PHPCS config", []string{"phpcs.xml"}}, {"Update coding styles", []string{"web/a.php"}}},
			Finding: "styled",
		}, nil
	})}}})

	require.NoError(t, Fire(p, siteEvent(t, committer)))

	assert.Equal(t, map[string]Recorded[string]{
		"default": {Finding: "styled", Committed: []string{"Add PHPCS config"}},
	}, Sites[string](p.findings[0], EventPostSiteUpdate))
}

func TestAProducedCommitIsMadeBeforeTheNextStageRuns(t *testing.T) {
	var ran []string
	committer := NewMockCommitter(t)
	committer.EXPECT().Commit("Remove deprecations", []string{"web/a.php"}).RunAndReturn(func(string, ...string) (bool, error) {
		ran = append(ran, "commit")
		return true, nil
	})
	p := NewPipeline([]Addon{
		hookAddon{Hooks{PostSiteUpdate: On(Format, func(*PostSiteUpdateEvent) error {
			ran = append(ran, "format")
			return nil
		})}},
		hookAddon{Hooks{PostSiteUpdate: Produce(Change, func(*PostSiteUpdateEvent) (Result[any], error) {
			ran = append(ran, "change")
			return Result[any]{Commits: []Commit{{"Remove deprecations", []string{"web/a.php"}}}}, nil
		})}},
	})

	require.NoError(t, Fire(p, siteEvent(t, committer)))

	assert.Equal(t, []string{"change", "commit", "format"}, ran)
}

func TestAFailedCommitStopsTheEventAndRecordsNothing(t *testing.T) {
	committer := NewMockCommitter(t)
	committer.EXPECT().Commit("Update translations", []string{"translations"}).Return(false, assert.AnError)
	later := false
	p := NewPipeline([]Addon{
		hookAddon{Hooks{PostSiteUpdate: Produce(Change, func(*PostSiteUpdateEvent) (Result[string], error) {
			return Result[string]{Commits: []Commit{{"Update translations", []string{"translations"}}}, Finding: "found"}, nil
		})}},
		hookAddon{Hooks{PostSiteUpdate: On(Inspect, func(*PostSiteUpdateEvent) error {
			later = true
			return nil
		})}},
	})

	err := Fire(p, siteEvent(t, committer))

	require.ErrorIs(t, err, assert.AnError)
	assert.Contains(t, err.Error(), "post-site-update hook failed")
	assert.Empty(t, Sites[string](p.findings[0], EventPostSiteUpdate))
	assert.False(t, later)
}

func TestAFailedProducedHookRecordsNothing(t *testing.T) {
	p := NewPipeline([]Addon{hookAddon{Hooks{PreSiteUpdate: Observe(func(*PreSiteUpdateEvent) (string, error) {
		return "found", assert.AnError
	})}}})

	require.ErrorIs(t, Fire(p, NewPreSiteUpdateEvent(t.Context(), "/project", nil, "default", drush.Facts{})), assert.AnError)
	assert.Empty(t, Sites[string](p.findings[0], EventPreSiteUpdate))
}

func TestObserveRunsAtInspectAndRecordsPerSite(t *testing.T) {
	hook := Observe(func(e *PreSiteUpdateEvent) (string, error) { return "seen " + e.Site(), nil })
	p := NewPipeline([]Addon{hookAddon{}, hookAddon{Hooks{PreSiteUpdate: hook}}})

	require.NoError(t, Fire(p, NewPreSiteUpdateEvent(t.Context(), "/project", nil, "default", drush.Facts{})))

	assert.Equal(t, Inspect, hook.Stage)
	assert.Equal(t, map[string]string{"default": "seen default"}, SiteFindings[string](p.findings[1], EventPreSiteUpdate))
	assert.Empty(t, Sites[string](p.findings[0], EventPreSiteUpdate), "each addon has findings of its own")
}

func TestHooksBuiltWithOnRecordNoFinding(t *testing.T) {
	p := NewPipeline([]Addon{hookAddon{Hooks{PostSiteUpdate: On(Change, func(*PostSiteUpdateEvent) error { return nil })}}})

	require.NoError(t, Fire(p, siteEvent(t, nil)))

	assert.Empty(t, Sites[any](p.findings[0], EventPostSiteUpdate))
}

func TestSitesReadsOnlyItsEventsPerSiteFindings(t *testing.T) {
	f := &Findings{}
	f.Record(EventPreSiteUpdate, "default", "before", nil)
	f.Record(EventPostSiteUpdate, "default", "after", nil)
	f.Record(EventPreSiteUpdate, "", "not per site", nil)

	assert.Equal(t, map[string]string{"default": "before"}, SiteFindings[string](f, EventPreSiteUpdate))
}

func TestSitesReturnsACopy(t *testing.T) {
	f := &Findings{}
	f.Record(EventPostSiteUpdate, "default", "found", []string{"Update translations"})

	Sites[string](f, EventPostSiteUpdate)["default"].Committed[0] = "changed"

	assert.Equal(t, []string{"Update translations"}, Sites[string](f, EventPostSiteUpdate)["default"].Committed)
}

func TestSitesPanicsOnAFindingOfAnotherType(t *testing.T) {
	f := &Findings{}
	f.Record(EventPreSiteUpdate, "default", 42, nil)

	assert.PanicsWithValue(t, "pre-site-update finding for site default is a int, not a string", func() {
		Sites[string](f, EventPreSiteUpdate)
	})
}

// Per-site hooks fire from a goroutine per site: under -race, this is what proves addons need no
// lock of their own.
func TestConcurrentSitesAllRecordTheirFinding(t *testing.T) {
	p := NewPipeline([]Addon{hookAddon{Hooks{PreSiteUpdate: Observe(func(e *PreSiteUpdateEvent) (string, error) {
		return e.Site(), nil
	})}}})
	sites := make([]string, 16)
	for i := range sites {
		sites[i] = "site" + strconv.Itoa(i)
	}

	var wg sync.WaitGroup
	start := make(chan struct{})
	for _, site := range sites {
		wg.Go(func() {
			<-start
			assert.NoError(t, Fire(p, NewPreSiteUpdateEvent(t.Context(), "/project", nil, site, drush.Facts{})))
		})
	}
	close(start)
	wg.Wait()

	found := SiteFindings[string](p.findings[0], EventPreSiteUpdate)
	require.Len(t, found, len(sites))
	for _, site := range sites {
		assert.Equal(t, site, found[site])
	}
}

type findingsAddon struct{ hookAddon }

func (findingsAddon) ReportKey() string { return "findings" }

func (findingsAddon) ReportData(f *Findings) any { return SiteFindings[string](f, EventPreSiteUpdate) }

func TestPipelineReportsOnlyReporters(t *testing.T) {
	p := NewPipeline([]Addon{
		findingsAddon{hookAddon{Hooks{PreSiteUpdate: Observe(func(*PreSiteUpdateEvent) (string, error) { return "found", nil })}}},
		hookAddon{Hooks{PreSiteUpdate: Observe(func(*PreSiteUpdateEvent) (string, error) { return "unreported", nil })}},
	})
	require.NoError(t, Fire(p, NewPreSiteUpdateEvent(t.Context(), "/project", nil, "default", drush.Facts{})))

	rec := report.NewRecorder("dev", report.ModeNormal, false, "", "", nil)
	p.Report(rec)

	assert.Equal(t, map[string]any{"findings": map[string]string{"default": "found"}}, rec.Snapshot().Addons)
}

func TestProducedProposalIsCommittedRecordedAndMerged(t *testing.T) {
	committer := NewMockCommitter(t)
	committer.EXPECT().Commit("Update patches", []string{"composer.json"}).Return(true, nil)
	pin := UpdatePlan{Keep: []Pin{pin("drupal/core", "10.2.0")}}
	hook := ProduceProposal(Change, func(*PreComposerUpdateEvent) (Proposal[string], error) {
		return Proposal[string]{Plan: pin, Commits: []Commit{{"Update patches", []string{"composer.json"}}}, Finding: "patched"}, nil
	})
	p := NewPipeline([]Addon{hookAddon{proposing(Prepare, UpdatePlan{}, nil)}, hookAddon{Hooks{PreComposerUpdate: hook}}})

	plan, err := Settle(p, NewPreComposerUpdateEvent(t.Context(), "/p", committer, UpdatePlan{}))

	require.NoError(t, err)
	assert.Equal(t, Change, hook.Stage)
	assert.Equal(t, pin, plan)
	recorded, ok := Shared[string](p.findings[1], EventPreComposerUpdate)
	require.True(t, ok)
	assert.Equal(t, Recorded[string]{Finding: "patched", Committed: []string{"Update patches"}}, recorded)
	_, ok = Shared[struct{}](p.findings[0], EventPreComposerUpdate)
	assert.True(t, ok, "every pre-composer-update hook records its finding, even an empty one")
}

func TestAFailedProposalCommitStopsTheSettle(t *testing.T) {
	committer := NewMockCommitter(t)
	committer.EXPECT().Commit("Update patches", []string{"composer.json"}).Return(false, assert.AnError)
	p := NewPipeline([]Addon{hookAddon{Hooks{PreComposerUpdate: ProduceProposal(Change, func(*PreComposerUpdateEvent) (Proposal[string], error) {
		return Proposal[string]{Plan: UpdatePlan{Only: []string{"drupal/core"}}, Commits: []Commit{{"Update patches", []string{"composer.json"}}}}, nil
	})}}})

	plan, err := Settle(p, NewPreComposerUpdateEvent(t.Context(), "/p", committer, UpdatePlan{}))

	require.ErrorIs(t, err, assert.AnError)
	assert.Contains(t, err.Error(), "pre-composer-update hook failed")
	assert.Zero(t, plan)
	_, ok := Shared[string](p.findings[0], EventPreComposerUpdate)
	assert.False(t, ok)
}

func TestSharedIgnoresPerSiteFindings(t *testing.T) {
	f := &Findings{}
	f.Record(EventPreComposerUpdate, "default", "per site", nil)

	_, ok := Shared[string](f, EventPreComposerUpdate)

	assert.False(t, ok)
}

func TestSharedPanicsOnAFindingOfAnotherType(t *testing.T) {
	f := &Findings{}
	f.Record(EventPreComposerUpdate, "", 42, nil)

	assert.PanicsWithValue(t, "pre-composer-update finding is a int, not a string", func() {
		Shared[string](f, EventPreComposerUpdate)
	})
}

func TestProduceFromReadsOnlyItsOwnAddonsEarlierFindings(t *testing.T) {
	var sawOwn, sawOther bool
	p := NewPipeline([]Addon{
		hookAddon{Hooks{PreComposerUpdate: ProduceProposal(Prepare, func(*PreComposerUpdateEvent) (Proposal[string], error) {
			return Proposal[string]{Finding: "other addon's"}, nil
		})}},
		hookAddon{Hooks{
			PreComposerUpdate: ProduceProposal(Change, func(*PreComposerUpdateEvent) (Proposal[string], error) {
				return Proposal[string]{Finding: "own"}, nil
			}),
			PostComposerUpdate: ProduceFrom(Change, func(_ *PostComposerUpdateEvent, earlier *Findings) (Result[string], error) {
				own, _ := Shared[string](earlier, EventPreComposerUpdate)
				sawOwn = own.Finding == "own"
				sawOther = own.Finding == "other addon's"
				return Result[string]{Finding: "after"}, nil
			}),
		}},
	})

	_, err := Settle(p, NewPreComposerUpdateEvent(t.Context(), "/p", nil, UpdatePlan{}))
	require.NoError(t, err)
	require.NoError(t, Fire(p, NewPostComposerUpdateEvent(t.Context(), "/p", nil)))

	assert.True(t, sawOwn)
	assert.False(t, sawOther)
	after, _ := Shared[string](p.findings[1], EventPostComposerUpdate)
	assert.Equal(t, "after", after.Finding)
}
