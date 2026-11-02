package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/drupdater/drupdater/internal/agent"
	"github.com/drupdater/drupdater/internal/buildinfo"
	"github.com/drupdater/drupdater/internal/repo"
	"github.com/drupdater/drupdater/internal/report"
)

var testAuthor = repo.Identity{Name: "Drupdater Bot", Email: "bot@example.com"}

// scriptedApp stands in for the app and the agent's report: it answers each event's
// internal:fire with what the test scripted for it, and records every request. An unscripted
// event fires no hook, and pre-composer-update hands back the plan it was given.
type scriptedApp struct {
	mu       sync.Mutex
	events   map[string]func(agent.FireRequest) (agent.FireReply, error)
	fired    []agent.FireRequest
	dirs     []string
	commit   func(agent.CommitRequest) (agent.CommitReply, error)
	commits  []agent.CommitRequest
	report   func(agent.ReportRequest) (agent.ReportReply, error)
	reported []agent.ReportRequest
}

func (s *scriptedApp) Fire(_ context.Context, dir string, request agent.FireRequest) (agent.FireReply, error) {
	s.mu.Lock()
	s.fired = append(s.fired, request)
	s.dirs = append(s.dirs, dir)
	fire := s.events[request.Event.Name]
	s.mu.Unlock()
	if fire == nil {
		return agent.FireReply{Plan: request.Event.Plan}, nil
	}
	return fire(request)
}

// Commit makes every commit it is asked for, unless the test scripted otherwise.
func (s *scriptedApp) Commit(_ context.Context, _ string, request agent.CommitRequest) (agent.CommitReply, error) {
	s.mu.Lock()
	s.commits = append(s.commits, request)
	s.mu.Unlock()
	if s.commit != nil {
		return s.commit(request)
	}
	var reply agent.CommitReply
	for _, c := range request.Commits {
		reply.Committed = append(reply.Committed, c.Message)
	}
	return reply, nil
}

func (s *scriptedApp) Report(_ context.Context, request agent.ReportRequest) (agent.ReportReply, error) {
	s.reported = append(s.reported, request)
	if s.report == nil {
		return agent.ReportReply{}, nil
	}
	return s.report(request)
}

// serve makes s answer what the runner asks c of the app and of the report.
func (s *scriptedApp) serve(c *MockComposer) {
	c.EXPECT().Fire(anyCtx, mock.Anything, mock.Anything).RunAndReturn(s.Fire).Maybe()
	c.EXPECT().Commit(anyCtx, mock.Anything, mock.Anything).RunAndReturn(s.Commit).Maybe()
	c.EXPECT().Report(anyCtx, mock.Anything).RunAndReturn(s.Report).Maybe()
}

// committed is the messages of every commit the run asked the app for, in order.
func (s *scriptedApp) committed() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var messages []string
	for _, r := range s.commits {
		for _, c := range r.Commits {
			messages = append(messages, c.Message)
		}
	}
	return messages
}

// addon hooks every event it names at its stage.
func addon(name string, hooks map[string]string) Addon {
	return Addon{Name: name, Hooks: hooks}
}

// hookingAll hooks every event.
func hookingAll(name string) Addon {
	hooks := map[string]string{}
	for _, e := range Events {
		hooks[e] = "change"
	}
	return addon(name, hooks)
}

// recording is an event whose hooks record finding for each addon named, and hand back the plan.
func recording(finding string, addons ...string) func(agent.FireRequest) (agent.FireReply, error) {
	return func(r agent.FireRequest) (agent.FireReply, error) {
		reply := agent.FireReply{Plan: r.Event.Plan}
		for _, a := range addons {
			reply.Recorded = append(reply.Recorded, agent.Recorded{Addon: a, Finding: agent.Finding{
				Event: r.Event.Name, Site: r.Event.Site, Finding: json.RawMessage(finding), Committed: []string{},
			}})
		}
		return reply, nil
	}
}

// failingOn is an addon whose hook of event fails, served to c.
func failingOn(c *MockComposer, event string) Addon {
	(&scriptedApp{events: map[string]func(agent.FireRequest) (agent.FireReply, error){
		event: func(agent.FireRequest) (agent.FireReply, error) {
			return agent.FireReply{Failure: event + " hook failed: failing: " + assert.AnError.Error()}, nil
		},
	}}).serve(c)
	return addon("failing", map[string]string{event: "change"})
}

func TestFireAsksTheAppWithTheEventTheAddonsTheRunAndTheAuthor(t *testing.T) {
	s := &scriptedApp{}
	p := NewPipeline([]Addon{
		addon("audit", map[string]string{EventPreComposerUpdate: "prepare"}),
		addon("patches", map[string]string{EventPreComposerUpdate: "change", EventPostComposerUpdate: "change"}),
	}, s, true)

	_, err := p.fire(t.Context(), "/project", testAuthor, preComposerUpdate(UpdatePlan{Only: []string{"a/b"}}))

	require.NoError(t, err)
	require.Len(t, s.fired, 1)
	assert.Equal(t, "/project", s.dirs[0])
	encoded, err := json.Marshal(s.fired[0])
	require.NoError(t, err)
	assert.JSONEq(t, `{
		"event": {"name": "pre-composer-update", "plan": {"only": ["a/b"], "keep": [], "minimal_changes": false}},
		"addons": [
			{"name": "audit", "hooks": {"pre-composer-update": "prepare"}, "findings": []},
			{"name": "patches", "hooks": {"pre-composer-update": "change", "post-composer-update": "change"}, "findings": []}
		],
		"run": {"security": true},
		"author": {"name": "Drupdater Bot", "email": "bot@example.com"}
	}`, string(encoded))
}

func TestAnEventNoAddonHooksStartsNoAppAndKeepsThePlan(t *testing.T) {
	s := &scriptedApp{}
	p := NewPipeline([]Addon{addon("a", map[string]string{EventPostCodeUpdate: "inspect"})}, s, false)

	plan, err := p.fire(t.Context(), "/p", testAuthor, preComposerUpdate(UpdatePlan{Only: []string{"x/y"}}))

	require.NoError(t, err)
	assert.Equal(t, UpdatePlan{Only: []string{"x/y"}}, plan)
	assert.Empty(t, s.fired)
}

func TestEachEventHandsTheAppItsOwnPayload(t *testing.T) {
	facts := json.RawMessage(`{"modules":["locale_deploy"]}`)
	for _, tt := range []struct {
		event event
		want  string
	}{
		{preComposerUpdate(UpdatePlan{Only: []string{"a/b"}, Keep: []Pin{{Package: "c/d", Version: "1.x-dev abc"}}}),
			`{"name":"pre-composer-update","plan":{"only":["a/b"],"keep":[{"package":"c/d","version":"1.x-dev abc"}],"minimal_changes":false}}`},
		{preComposerUpdate(UpdatePlan{}), `{"name":"pre-composer-update","plan":{"only":[],"keep":[],"minimal_changes":false}}`},
		{postComposerUpdate(json.RawMessage(`{"packages":[]}`)), `{"name":"post-composer-update","base_lock":{"packages":[]}}`},
		{postCodeUpdate(), `{"name":"post-code-update"}`},
		{preSiteUpdate("default", facts), `{"name":"pre-site-update","site":"default","facts":{"modules":["locale_deploy"]}}`},
		{postSiteUpdate("second", facts), `{"name":"post-site-update","site":"second","facts":{"modules":["locale_deploy"]}}`},
	} {
		t.Run(tt.event.name, func(t *testing.T) {
			s := &scriptedApp{}
			p := NewPipeline([]Addon{addon("a", map[string]string{tt.event.name: "change"})}, s, false)

			_, err := p.fire(t.Context(), "/p", testAuthor, tt.event)

			require.NoError(t, err)
			encoded, err := json.Marshal(s.fired[0].Event)
			require.NoError(t, err)
			assert.JSONEq(t, tt.want, string(encoded))
		})
	}
}

func TestPreComposerUpdateReturnsThePlanTheAppMerged(t *testing.T) {
	s := &scriptedApp{events: map[string]func(agent.FireRequest) (agent.FireReply, error){
		EventPreComposerUpdate: func(agent.FireRequest) (agent.FireReply, error) {
			return agent.FireReply{Plan: &agent.Plan{Only: []string{"drupal/core"}, Keep: []agent.Pin{{Package: "drupal/token", Version: "1.13.0"}}, MinimalChanges: true}}, nil
		},
	}}
	p := NewPipeline([]Addon{hookingAll("a")}, s, false)

	plan, err := p.fire(t.Context(), "/p", testAuthor, preComposerUpdate(UpdatePlan{}))

	require.NoError(t, err)
	assert.Equal(t, UpdatePlan{Only: []string{"drupal/core"}, Keep: []Pin{{Package: "drupal/token", Version: "1.13.0"}}, MinimalChanges: true}, plan)
}

func TestPreComposerUpdateWithoutAPlanFromTheAppFails(t *testing.T) {
	s := &scriptedApp{events: map[string]func(agent.FireRequest) (agent.FireReply, error){
		EventPreComposerUpdate: func(agent.FireRequest) (agent.FireReply, error) { return agent.FireReply{}, nil },
	}}

	_, err := NewPipeline([]Addon{hookingAll("a")}, s, false).fire(t.Context(), "/p", testAuthor, preComposerUpdate(UpdatePlan{}))

	require.EqualError(t, err, "the app answered pre-composer-update without a plan")
}

func TestAnyOtherEventReturnsNoPlan(t *testing.T) {
	s := &scriptedApp{events: map[string]func(agent.FireRequest) (agent.FireReply, error){
		EventPostCodeUpdate: func(agent.FireRequest) (agent.FireReply, error) {
			return agent.FireReply{Plan: &agent.Plan{Only: []string{"x/y"}}}, nil
		},
	}}

	plan, err := NewPipeline([]Addon{hookingAll("a")}, s, false).fire(t.Context(), "/p", testAuthor, postCodeUpdate())

	require.NoError(t, err)
	assert.Zero(t, plan)
}

func TestAFailedEventFailsWithTheAppsReasonKeepingWhatItRecorded(t *testing.T) {
	s := &scriptedApp{events: map[string]func(agent.FireRequest) (agent.FireReply, error){
		EventPreSiteUpdate: func(r agent.FireRequest) (agent.FireReply, error) {
			reply, _ := recording(`"found"`, "first")(r)
			reply.Failure = "pre-site-update hook failed: broken: boom"
			return reply, nil
		},
	}}
	p := NewPipeline([]Addon{hookingAll("first"), hookingAll("broken")}, s, false)

	_, err := p.fire(t.Context(), "/p", testAuthor, preSiteUpdate("default", nil))

	require.EqualError(t, err, "pre-site-update hook failed: broken: boom")
	assert.Equal(t, []agent.Finding{{Event: EventPreSiteUpdate, Site: "default", Finding: json.RawMessage(`"found"`), Committed: []string{}}}, p.findings[0])
}

func TestAnAbortEndsTheRunWithoutFailingIt(t *testing.T) {
	s := &scriptedApp{events: map[string]func(agent.FireRequest) (agent.FireReply, error){
		EventPreComposerUpdate: func(agent.FireRequest) (agent.FireReply, error) {
			return agent.FireReply{Abort: "No security advisories found"}, nil
		},
	}}

	_, err := NewPipeline([]Addon{hookingAll("a")}, s, true).fire(t.Context(), "/p", testAuthor, preComposerUpdate(UpdatePlan{}))

	abort, ok := errors.AsType[AbortError](err)
	require.True(t, ok, "%v", err)
	assert.Equal(t, "No security advisories found", abort.Msg)
	assert.EqualError(t, err, "pre-composer-update hook failed: No security advisories found")
}

func TestAnAppThatCannotAnswerFailsTheEvent(t *testing.T) {
	s := &scriptedApp{events: map[string]func(agent.FireRequest) (agent.FireReply, error){
		EventPostCodeUpdate: func(agent.FireRequest) (agent.FireReply, error) { return agent.FireReply{}, assert.AnError },
	}}

	_, err := NewPipeline([]Addon{hookingAll("a")}, s, false).fire(t.Context(), "/p", testAuthor, postCodeUpdate())

	require.ErrorIs(t, err, assert.AnError)
	assert.ErrorContains(t, err, "failed to fire post-code-update: ")
}

func TestAFindingOfAnAddonTheRunDoesNotRunIsRefused(t *testing.T) {
	s := &scriptedApp{events: map[string]func(agent.FireRequest) (agent.FireReply, error){
		EventPostCodeUpdate: recording(`1`, "stranger"),
	}}

	_, err := NewPipeline([]Addon{hookingAll("a")}, s, false).fire(t.Context(), "/p", testAuthor, postCodeUpdate())

	require.EqualError(t, err, `the app recorded a finding of "stranger", which the run does not run`)
}

func TestTheAppIsHandedEachAddonsOwnFindingsSoFar(t *testing.T) {
	s := &scriptedApp{events: map[string]func(agent.FireRequest) (agent.FireReply, error){
		EventPreComposerUpdate: recording(`"pre"`, "mine"),
		EventPreSiteUpdate: func(r agent.FireRequest) (agent.FireReply, error) {
			return recording(`"`+r.Event.Site+`"`, "mine", "other")(r)
		},
	}}
	p := NewPipeline([]Addon{hookingAll("mine"), hookingAll("other")}, s, false)

	_, err := p.fire(t.Context(), "/p", testAuthor, preComposerUpdate(UpdatePlan{}))
	require.NoError(t, err)
	_, err = p.fire(t.Context(), "/p", testAuthor, preSiteUpdate("zeta", nil))
	require.NoError(t, err)
	_, err = p.fire(t.Context(), "/p", testAuthor, postCodeUpdate())
	require.NoError(t, err)

	last := s.fired[len(s.fired)-1].Addons
	assert.Equal(t, []agent.Finding{
		{Event: EventPreComposerUpdate, Finding: json.RawMessage(`"pre"`), Committed: []string{}},
		{Event: EventPreSiteUpdate, Site: "zeta", Finding: json.RawMessage(`"zeta"`), Committed: []string{}},
	}, last[0].Findings)
	assert.Equal(t, []agent.Finding{{Event: EventPreSiteUpdate, Site: "zeta", Finding: json.RawMessage(`"zeta"`), Committed: []string{}}}, last[1].Findings)
	assert.Equal(t, []agent.Finding{}, s.fired[0].Addons[1].Findings, "an addon that found nothing yet gets an empty list, not none")
}

func TestARepeatedHookReplacesItsFinding(t *testing.T) {
	n := 0
	s := &scriptedApp{events: map[string]func(agent.FireRequest) (agent.FireReply, error){
		EventPostCodeUpdate: func(r agent.FireRequest) (agent.FireReply, error) { n++; return recording(fmt.Sprint(n), "a")(r) },
	}}
	p := NewPipeline([]Addon{hookingAll("a")}, s, false)

	for range 2 {
		_, err := p.fire(t.Context(), "/p", testAuthor, postCodeUpdate())
		require.NoError(t, err)
	}

	assert.Equal(t, []agent.Finding{{Event: EventPostCodeUpdate, Finding: json.RawMessage(`2`), Committed: []string{}}}, p.findings[0])
}

func TestConcurrentSitesAllRecordTheirFinding(t *testing.T) {
	s := &scriptedApp{events: map[string]func(agent.FireRequest) (agent.FireReply, error){
		EventPreSiteUpdate: recording(`1`, "a"),
	}}
	p := NewPipeline([]Addon{hookingAll("a")}, s, false)

	var wg sync.WaitGroup
	for i := range 20 {
		wg.Go(func() {
			_, err := p.fire(t.Context(), "/p", testAuthor, preSiteUpdate(fmt.Sprintf("site%d", i), nil))
			assert.NoError(t, err)
		})
	}
	wg.Wait()

	assert.Len(t, p.findings[0], 20)
}

func TestTheReportIsBuiltFromEveryAddonsFindingsInAddonOrder(t *testing.T) {
	s := &scriptedApp{
		events: map[string]func(agent.FireRequest) (agent.FireReply, error){EventPostComposerUpdate: recording(`["row"]`, "diff")},
		report: func(agent.ReportRequest) (agent.ReportReply, error) {
			return agent.ReportReply{Sections: map[string]json.RawMessage{"diff": json.RawMessage(`["row"]`)}}, nil
		},
	}
	p := NewPipeline([]Addon{hookingAll("diff"), hookingAll("quiet")}, s, false)
	_, err := p.fire(t.Context(), "/p", testAuthor, postComposerUpdate(json.RawMessage(`{}`)))
	require.NoError(t, err)
	rec := report.NewRecorder(buildinfo.Version, report.ModeNormal, false, "", "", nil)

	require.NoError(t, p.Report(t.Context(), rec))

	assert.Equal(t, []agent.ReportRequest{{Addons: []agent.AddonFindings{
		{Addon: "diff", Findings: []agent.Finding{{Event: EventPostComposerUpdate, Finding: json.RawMessage(`["row"]`), Committed: []string{}}}},
		{Addon: "quiet"},
	}}}, s.reported)
	assert.Equal(t, map[string]any{"diff": json.RawMessage(`["row"]`)}, rec.Snapshot().Addons)
}

func TestTheReportAsksTheAgentOnlyWhenThereIsSomethingNew(t *testing.T) {
	s := &scriptedApp{events: map[string]func(agent.FireRequest) (agent.FireReply, error){EventPostCodeUpdate: recording(`1`, "a")}}
	p := NewPipeline([]Addon{hookingAll("a")}, s, false)
	rec := report.NewRecorder(buildinfo.Version, report.ModeNormal, false, "", "", nil)

	require.NoError(t, p.Report(t.Context(), rec))
	assert.Empty(t, s.reported, "a run that never reached a hook has no agent to ask")

	_, err := p.fire(t.Context(), "/p", testAuthor, postCodeUpdate())
	require.NoError(t, err)
	require.NoError(t, p.Report(t.Context(), rec))
	require.NoError(t, p.Report(t.Context(), rec))
	assert.Len(t, s.reported, 1)
}

func TestTheReportFailsWithTheAgentAndOnASectionOfAnAddonTheRunDidNotRun(t *testing.T) {
	for name, tt := range map[string]struct {
		reply agent.ReportReply
		err   error
		want  string
	}{
		"the agent fails":   {err: assert.AnError, want: assert.AnError.Error()},
		"a foreign section": {reply: agent.ReportReply{Sections: map[string]json.RawMessage{"z": nil, "b": nil}}, want: "the agent reported sections of addons the run did not run: [b z]"},
	} {
		t.Run(name, func(t *testing.T) {
			s := &scriptedApp{
				events: map[string]func(agent.FireRequest) (agent.FireReply, error){EventPostCodeUpdate: recording(`1`, "a")},
				report: func(agent.ReportRequest) (agent.ReportReply, error) { return tt.reply, tt.err },
			}
			p := NewPipeline([]Addon{hookingAll("a")}, s, false)
			_, err := p.fire(t.Context(), "/p", testAuthor, postCodeUpdate())
			require.NoError(t, err)

			err = p.Report(t.Context(), report.NewRecorder(buildinfo.Version, report.ModeNormal, false, "", "", nil))

			require.EqualError(t, err, tt.want)
		})
	}
}

func TestAbortError(t *testing.T) {
	assert.Equal(t, "something aborted", AbortError{Msg: "something aborted"}.Error())
}
