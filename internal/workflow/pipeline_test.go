package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/drupdater/drupdater/internal/agent"
	"github.com/drupdater/drupdater/internal/buildinfo"
	"github.com/drupdater/drupdater/internal/drush"
	"github.com/drupdater/drupdater/internal/report"
)

type recordedCommit struct {
	message string
	paths   []string
}

// fakeCommitter records what it was asked to commit and answers with result and err.
type fakeCommitter struct {
	mu      sync.Mutex
	commits []recordedCommit
	result  bool
	err     error
}

func (f *fakeCommitter) Commit(_ context.Context, message string, paths ...string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.commits = append(f.commits, recordedCommit{message: message, paths: paths})
	return f.result, f.err
}

// scriptedAgent answers each addon's hook with what the test scripted for it, and records the
// requests, in the order they came.
type scriptedAgent struct {
	mu       sync.Mutex
	hooks    map[string]func(agent.HookRequest) (agent.HookReply, error)
	requests []agent.HookRequest
	dirs     []string
	report   func(agent.ReportRequest) (agent.ReportReply, error)
	reported []agent.ReportRequest
}

func (s *scriptedAgent) RunHook(_ context.Context, dir string, request agent.HookRequest) (agent.HookReply, error) {
	s.mu.Lock()
	s.requests = append(s.requests, request)
	s.dirs = append(s.dirs, dir)
	hook := s.hooks[request.Addon]
	s.mu.Unlock()
	if hook == nil {
		return agent.HookReply{Finding: json.RawMessage("null")}, nil
	}
	return hook(request)
}

func (s *scriptedAgent) Report(_ context.Context, request agent.ReportRequest) (agent.ReportReply, error) {
	s.reported = append(s.reported, request)
	if s.report == nil {
		return agent.ReportReply{}, nil
	}
	return s.report(request)
}

// called is the addons whose hooks ran, in order.
func (s *scriptedAgent) called() []string {
	var names []string
	for _, r := range s.requests {
		names = append(names, r.Addon)
	}
	return names
}

// addon hooks every event it names at its stage.
func addon(name string, hooks map[string]Stage) Addon {
	return Addon{Name: name, Hooks: hooks}
}

func finding(v any) agent.HookReply {
	raw, _ := json.Marshal(v)
	return agent.HookReply{Finding: raw}
}

func TestHooksRunByStageThenAddonOrder(t *testing.T) {
	s := &scriptedAgent{}
	p := NewPipeline([]Addon{
		addon("audit", map[string]Stage{EventPostCodeUpdate: Inspect}),
		addon("phpcbf", map[string]Stage{EventPostCodeUpdate: Format}),
		addon("bystander", map[string]Stage{EventPreSiteUpdate: Change}),
		addon("rector", map[string]Stage{EventPostCodeUpdate: Change}),
		addon("first", map[string]Stage{EventPostCodeUpdate: Prepare}),
		addon("second change", map[string]Stage{EventPostCodeUpdate: Change}),
	}, s, false)

	_, err := p.fire(t.Context(), "/project", &fakeCommitter{}, postCodeUpdate())

	require.NoError(t, err)
	assert.Equal(t, []string{"first", "rector", "second change", "phpcbf", "audit"}, s.called())
	assert.Equal(t, "/project", s.dirs[0])
}

func TestHooksStopAtTheFirstFailure(t *testing.T) {
	s := &scriptedAgent{hooks: map[string]func(agent.HookRequest) (agent.HookReply, error){
		"broken": func(agent.HookRequest) (agent.HookReply, error) { return agent.HookReply{}, assert.AnError },
	}}
	p := NewPipeline([]Addon{
		addon("broken", map[string]Stage{EventPreSiteUpdate: Change}),
		addon("later", map[string]Stage{EventPreSiteUpdate: Inspect}),
	}, s, false)

	_, err := p.fire(t.Context(), "/p", &fakeCommitter{}, preSiteUpdate("default", drush.Facts{Raw: json.RawMessage(`{}`)}))

	require.ErrorIs(t, err, assert.AnError)
	require.EqualError(t, err, "pre-site-update hook failed: broken: "+assert.AnError.Error())
	assert.Equal(t, []string{"broken"}, s.called())
}

func TestEachEventHandsItsHooksItsOwnPayload(t *testing.T) {
	facts := drush.Facts{ConfigSyncDirectory: "/p/config", Raw: json.RawMessage(`{"modules":["locale_deploy"]}`)}
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
			s := &scriptedAgent{}
			p := NewPipeline([]Addon{addon("a", map[string]Stage{tt.event.name: Change})}, s, true)

			_, err := p.fire(t.Context(), "/p", &fakeCommitter{}, tt.event)

			require.NoError(t, err)
			require.Len(t, s.requests, 1)
			encoded, err := json.Marshal(s.requests[0].Event)
			require.NoError(t, err)
			assert.JSONEq(t, tt.want, string(encoded))
			assert.Equal(t, agent.Run{Security: true}, s.requests[0].Run)
		})
	}
}

func TestPreComposerUpdateMergesEveryProposal(t *testing.T) {
	s := &scriptedAgent{hooks: map[string]func(agent.HookRequest) (agent.HookReply, error){
		"audit":   proposing(agent.Plan{Only: []string{"drupal/core", "drupal/token"}, MinimalChanges: true}),
		"patches": proposing(agent.Plan{Keep: []agent.Pin{{Package: "drupal/token", Version: "1.13.0"}}}),
	}}
	p := NewPipeline([]Addon{
		addon("audit", map[string]Stage{EventPreComposerUpdate: Prepare}),
		addon("patches", map[string]Stage{EventPreComposerUpdate: Change}),
		addon("bystander", map[string]Stage{EventPreComposerUpdate: Change}),
	}, s, false)

	plan, err := p.fire(t.Context(), "/p", &fakeCommitter{}, preComposerUpdate(UpdatePlan{}))

	require.NoError(t, err)
	assert.Equal(t, UpdatePlan{Only: []string{"drupal/core", "drupal/token"}, Keep: []Pin{{Package: "drupal/token", Version: "1.13.0"}}, MinimalChanges: true}, plan)
}

func TestPreComposerUpdateHooksSeeOnlyEarlierStages(t *testing.T) {
	s := &scriptedAgent{hooks: map[string]func(agent.HookRequest) (agent.HookReply, error){
		"narrow":       proposing(agent.Plan{Only: []string{"drupal/core"}}),
		"first change": proposing(agent.Plan{Keep: []agent.Pin{{Package: "drupal/core", Version: "10.3.0"}}}),
	}}
	p := NewPipeline([]Addon{
		addon("first change", map[string]Stage{EventPreComposerUpdate: Change}),
		addon("inspect", map[string]Stage{EventPreComposerUpdate: Inspect}),
		addon("narrow", map[string]Stage{EventPreComposerUpdate: Prepare}),
		addon("second change", map[string]Stage{EventPreComposerUpdate: Change}),
	}, s, false)

	_, err := p.fire(t.Context(), "/p", &fakeCommitter{}, preComposerUpdate(UpdatePlan{}))

	require.NoError(t, err)
	seen := map[string]*agent.Plan{}
	for _, r := range s.requests {
		seen[r.Addon] = r.Event.Plan
	}
	narrowed := &agent.Plan{Only: []string{"drupal/core"}, Keep: []agent.Pin{}}
	assert.Equal(t, &agent.Plan{Only: []string{}, Keep: []agent.Pin{}}, seen["narrow"])
	assert.Equal(t, narrowed, seen["first change"])
	assert.Equal(t, narrowed, seen["second change"], "a stage-mate's proposal is not yet settled")
	assert.Equal(t, &agent.Plan{Only: []string{"drupal/core"}, Keep: []agent.Pin{{Package: "drupal/core", Version: "10.3.0"}}}, seen["inspect"])
}

func TestPreComposerUpdateFailsWhenProposalsDisagree(t *testing.T) {
	s := &scriptedAgent{hooks: map[string]func(agent.HookRequest) (agent.HookReply, error){
		"a": proposing(agent.Plan{Keep: []agent.Pin{{Package: "drupal/core", Version: "10.2.0"}}}),
		"b": proposing(agent.Plan{Keep: []agent.Pin{{Package: "drupal/core", Version: "10.3.0"}}}),
	}}
	p := NewPipeline([]Addon{
		addon("a", map[string]Stage{EventPreComposerUpdate: Change}),
		addon("b", map[string]Stage{EventPreComposerUpdate: Change}),
	}, s, false)

	plan, err := p.fire(t.Context(), "/p", &fakeCommitter{}, preComposerUpdate(UpdatePlan{}))

	require.ErrorContains(t, err, "pre-composer-update hooks disagree: drupal/core is pinned at both 10.2.0 and 10.3.0")
	assert.Zero(t, plan)
}

func TestAPlanOnAnyOtherEventIsRefused(t *testing.T) {
	s := &scriptedAgent{hooks: map[string]func(agent.HookRequest) (agent.HookReply, error){
		"a": proposing(agent.Plan{Only: []string{"x/y"}}),
	}}
	p := NewPipeline([]Addon{addon("a", map[string]Stage{EventPostCodeUpdate: Change})}, s, false)

	_, err := p.fire(t.Context(), "/p", &fakeCommitter{}, postCodeUpdate())

	require.EqualError(t, err, "post-code-update hook failed: a proposed a plan, which only pre-composer-update reads")
}

func TestAnAbortEndsTheRunWithoutFailingItOrRunningLaterHooks(t *testing.T) {
	s := &scriptedAgent{hooks: map[string]func(agent.HookRequest) (agent.HookReply, error){
		"audit": func(agent.HookRequest) (agent.HookReply, error) {
			return agent.HookReply{Abort: "No security advisories found"}, nil
		},
	}}
	p := NewPipeline([]Addon{
		addon("audit", map[string]Stage{EventPreComposerUpdate: Prepare}),
		addon("later", map[string]Stage{EventPreComposerUpdate: Change}),
	}, s, true)

	_, err := p.fire(t.Context(), "/p", &fakeCommitter{}, preComposerUpdate(UpdatePlan{}))

	abort, ok := errorsAsAbort(err)
	require.True(t, ok, "%v", err)
	assert.Equal(t, "No security advisories found", abort.Msg)
	assert.Equal(t, []string{"audit"}, s.called())
	assert.Equal(t, 0, p.changes, "an abort records nothing")
}

func TestCommitsAreMadeInOrderBeforeTheNextHookAndRecordedWhenMade(t *testing.T) {
	committer := &committerMaking{made: map[string]bool{"Update coding styles": true}}
	var sawCommits []string
	s := &scriptedAgent{hooks: map[string]func(agent.HookRequest) (agent.HookReply, error){
		"beautifier": func(agent.HookRequest) (agent.HookReply, error) {
			return agent.HookReply{Finding: json.RawMessage(`{"fixable":1}`), Commits: []agent.Commit{
				{Message: "Add PHPCS config", Paths: []string{"phpcs.xml"}},
				{Message: "Update coding styles", Paths: []string{"a.php"}},
			}}, nil
		},
		"inspector": func(agent.HookRequest) (agent.HookReply, error) {
			sawCommits = slices.Clone(committer.messages)
			return agent.HookReply{}, nil
		},
	}}
	p := NewPipeline([]Addon{
		addon("beautifier", map[string]Stage{EventPostCodeUpdate: Format}),
		addon("inspector", map[string]Stage{EventPostCodeUpdate: Inspect}),
	}, s, false)

	_, err := p.fire(t.Context(), "/p", committer, postCodeUpdate())

	require.NoError(t, err)
	assert.Equal(t, []string{"Add PHPCS config", "Update coding styles"}, sawCommits, "both are committed, in order, before the next stage runs")
	assert.Equal(t, []agent.Finding{{Event: EventPostCodeUpdate, Finding: json.RawMessage(`{"fixable":1}`), Committed: []string{"Update coding styles"}}}, p.findings[0])
}

func TestAFailedCommitStopsTheEventAndRecordsNothing(t *testing.T) {
	s := &scriptedAgent{hooks: map[string]func(agent.HookRequest) (agent.HookReply, error){
		"a": func(agent.HookRequest) (agent.HookReply, error) {
			return agent.HookReply{Commits: []agent.Commit{{Message: "m", Paths: []string{"x"}}}}, nil
		},
	}}
	p := NewPipeline([]Addon{addon("a", map[string]Stage{EventPostSiteUpdate: Change}), addon("b", map[string]Stage{EventPostSiteUpdate: Inspect})}, s, false)

	_, err := p.fire(t.Context(), "/p", &fakeCommitter{err: assert.AnError}, postSiteUpdate("default", drush.Facts{}))

	require.ErrorIs(t, err, assert.AnError)
	assert.Equal(t, []string{"a"}, s.called())
	assert.Empty(t, p.findings[0])
}

func TestAHookReadsBackOnlyItsOwnAddonsEarlierFindingsInEventAndSiteOrder(t *testing.T) {
	s := &scriptedAgent{hooks: map[string]func(agent.HookRequest) (agent.HookReply, error){
		"mine":  func(r agent.HookRequest) (agent.HookReply, error) { return finding(r.Event.Name + r.Event.Site), nil },
		"other": func(agent.HookRequest) (agent.HookReply, error) { return finding("theirs"), nil },
	}}
	p := NewPipeline([]Addon{
		addon("mine", map[string]Stage{EventPreComposerUpdate: Change, EventPreSiteUpdate: Change, EventPostCodeUpdate: Change}),
		addon("other", map[string]Stage{EventPreComposerUpdate: Prepare}),
	}, s, false)
	committer := &fakeCommitter{}

	_, err := p.fire(t.Context(), "/p", committer, preComposerUpdate(UpdatePlan{}))
	require.NoError(t, err)
	for _, site := range []string{"zeta", "alpha"} {
		_, err = p.fire(t.Context(), "/p", committer, preSiteUpdate(site, drush.Facts{}))
		require.NoError(t, err)
	}
	_, err = p.fire(t.Context(), "/p", committer, postCodeUpdate())
	require.NoError(t, err)

	last := s.requests[len(s.requests)-1]
	assert.Equal(t, []agent.Finding{
		{Event: EventPreComposerUpdate, Finding: json.RawMessage(`"pre-composer-update"`), Committed: []string{}},
		{Event: EventPreSiteUpdate, Site: "alpha", Finding: json.RawMessage(`"pre-site-updatealpha"`), Committed: []string{}},
		{Event: EventPreSiteUpdate, Site: "zeta", Finding: json.RawMessage(`"pre-site-updatezeta"`), Committed: []string{}},
	}, last.Earlier)
	assert.Equal(t, []agent.Finding{}, s.requests[0].Earlier, "a first hook gets an empty list, not none")
}

func TestARepeatedHookReplacesItsFinding(t *testing.T) {
	n := 0
	s := &scriptedAgent{hooks: map[string]func(agent.HookRequest) (agent.HookReply, error){
		"a": func(agent.HookRequest) (agent.HookReply, error) { n++; return finding(n), nil },
	}}
	p := NewPipeline([]Addon{addon("a", map[string]Stage{EventPostCodeUpdate: Change})}, s, false)

	for range 2 {
		_, err := p.fire(t.Context(), "/p", &fakeCommitter{}, postCodeUpdate())
		require.NoError(t, err)
	}

	assert.Equal(t, []agent.Finding{{Event: EventPostCodeUpdate, Finding: json.RawMessage(`2`), Committed: []string{}}}, p.findings[0])
}

func TestConcurrentSitesAllRecordTheirFinding(t *testing.T) {
	s := &scriptedAgent{hooks: map[string]func(agent.HookRequest) (agent.HookReply, error){
		"a": func(r agent.HookRequest) (agent.HookReply, error) { return finding(r.Event.Site), nil },
	}}
	p := NewPipeline([]Addon{addon("a", map[string]Stage{EventPreSiteUpdate: Inspect})}, s, false)

	var wg sync.WaitGroup
	for i := range 20 {
		wg.Go(func() {
			_, err := p.fire(t.Context(), "/p", &fakeCommitter{}, preSiteUpdate(fmt.Sprintf("site%d", i), drush.Facts{}))
			assert.NoError(t, err)
		})
	}
	wg.Wait()

	assert.Len(t, p.findings[0], 20)
}

func TestTheReportIsBuiltFromEveryAddonsFindingsInAddonOrder(t *testing.T) {
	s := &scriptedAgent{
		hooks: map[string]func(agent.HookRequest) (agent.HookReply, error){
			"diff": func(agent.HookRequest) (agent.HookReply, error) { return finding([]string{"row"}), nil },
		},
		report: func(agent.ReportRequest) (agent.ReportReply, error) {
			return agent.ReportReply{Sections: map[string]json.RawMessage{"diff": json.RawMessage(`["row"]`)}}, nil
		},
	}
	p := NewPipeline([]Addon{
		addon("diff", map[string]Stage{EventPostComposerUpdate: Inspect}),
		addon("quiet", map[string]Stage{EventPostCodeUpdate: Inspect}),
	}, s, false)
	_, err := p.fire(t.Context(), "/p", &fakeCommitter{}, postComposerUpdate(json.RawMessage(`{}`)))
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
	s := &scriptedAgent{}
	p := NewPipeline([]Addon{addon("a", map[string]Stage{EventPostCodeUpdate: Inspect})}, s, false)
	rec := report.NewRecorder(buildinfo.Version, report.ModeNormal, false, "", "", nil)

	require.NoError(t, p.Report(t.Context(), rec))
	assert.Empty(t, s.reported, "a run that never reached a hook has no agent to ask")

	_, err := p.fire(t.Context(), "/p", &fakeCommitter{}, postCodeUpdate())
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
			s := &scriptedAgent{report: func(agent.ReportRequest) (agent.ReportReply, error) { return tt.reply, tt.err }}
			p := NewPipeline([]Addon{addon("a", map[string]Stage{EventPostCodeUpdate: Inspect})}, s, false)
			_, err := p.fire(t.Context(), "/p", &fakeCommitter{}, postCodeUpdate())
			require.NoError(t, err)

			err = p.Report(t.Context(), report.NewRecorder(buildinfo.Version, report.ModeNormal, false, "", "", nil))

			require.EqualError(t, err, tt.want)
		})
	}
}

func TestParseStageReadsTheCataloguesNames(t *testing.T) {
	for name, stage := range map[string]Stage{"prepare": Prepare, "change": Change, "format": Format, "inspect": Inspect} {
		got, err := ParseStage(name)
		require.NoError(t, err)
		assert.Equal(t, stage, got)
	}
	_, err := ParseStage("later")
	require.EqualError(t, err, `unknown stage "later"`)
}

func TestAbortError(t *testing.T) {
	assert.Equal(t, "something aborted", AbortError{Msg: "something aborted"}.Error())
}

func proposing(plan agent.Plan) func(agent.HookRequest) (agent.HookReply, error) {
	return func(agent.HookRequest) (agent.HookReply, error) {
		return agent.HookReply{Plan: &plan}, nil
	}
}

func errorsAsAbort(err error) (AbortError, bool) {
	return errors.AsType[AbortError](err)
}

// committerMaking makes only the commits named in made, and records every one it was asked for.
type committerMaking struct {
	made     map[string]bool
	messages []string
}

func (c *committerMaking) Commit(_ context.Context, message string, _ ...string) (bool, error) {
	c.messages = append(c.messages, message)
	return c.made[message], nil
}

// serve makes s answer the hooks and the report the runner asks c for.
func (s *scriptedAgent) serve(c *MockComposer) {
	c.EXPECT().RunHook(anyCtx, mock.Anything, mock.Anything).RunAndReturn(s.RunHook).Maybe()
	c.EXPECT().Report(anyCtx, mock.Anything).RunAndReturn(s.Report).Maybe()
}

// failingOn is an addon whose hook of event fails, served to c.
func failingOn(c *MockComposer, event string) Addon {
	(&scriptedAgent{hooks: map[string]func(agent.HookRequest) (agent.HookReply, error){
		"failing": func(agent.HookRequest) (agent.HookReply, error) { return agent.HookReply{}, assert.AnError },
	}}).serve(c)
	return addon("failing", map[string]Stage{event: Change})
}
