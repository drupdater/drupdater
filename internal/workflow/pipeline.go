package workflow

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"sync"

	"github.com/drupdater/drupdater/internal/agent"
	"github.com/drupdater/drupdater/internal/report"
)

// AddonAgent runs the addons' hooks and builds their report sections: the Composer agent.
type AddonAgent interface {
	RunHook(ctx context.Context, dir string, request agent.HookRequest) (agent.HookReply, error)
	Report(ctx context.Context, request agent.ReportRequest) (agent.ReportReply, error)
}

// Pipeline runs a set of addons' hooks: in stage order, each in the agent, committing what each
// asks for before the next runs, and keeping what each found for its addon's later hooks and its
// report section. It stops at the first failing hook, so a later one never sees a half-done event.
type Pipeline struct {
	addons []Addon
	agent  AddonAgent
	run    agent.Run

	// Per-site hooks fire concurrently; this lock is the only one between them.
	mu       sync.Mutex
	findings [][]agent.Finding
	// changes counts recorded findings, so Report asks the agent again only when there are new ones.
	changes  int
	reported int
}

// NewPipeline runs addons in this order within a stage: the catalogue's mandatory ones first.
func NewPipeline(addons []Addon, addonAgent AddonAgent, security bool) *Pipeline {
	return &Pipeline{addons: addons, agent: addonAgent, run: agent.Run{Security: security}, findings: make([][]agent.Finding, len(addons))}
}

// fire runs every addon's hook for e in the project in dir. On pre-composer-update it returns e's
// plan merged with every hook's proposal; each hook sees the plan as earlier stages settled it,
// so hooks sharing a stage cannot see each other's proposals.
func (p *Pipeline) fire(ctx context.Context, dir string, committer Committer, e event) (UpdatePlan, error) {
	hooks := p.ordered(e.name)
	plan := e.plan.clone()
	settled := plan
	for i, h := range hooks {
		if i > 0 && h.stage != hooks[i-1].stage {
			settled = plan
		}
		name := p.addons[h.addon].Name
		reply, err := p.agent.RunHook(ctx, dir, agent.HookRequest{Addon: name, Event: e.payload(settled), Earlier: p.earlier(h.addon), Run: p.run})
		if err != nil {
			return UpdatePlan{}, fmt.Errorf("%s hook failed: %s: %w", e.name, name, err)
		}
		if reply.Abort != "" {
			return UpdatePlan{}, fmt.Errorf("%s hook failed: %w", e.name, AbortError{Msg: reply.Abort})
		}
		if reply.Plan != nil && e.name != EventPreComposerUpdate {
			return UpdatePlan{}, fmt.Errorf("%s hook failed: %s proposed a plan, which only pre-composer-update reads", e.name, name)
		}
		committed, err := commitAll(ctx, committer, reply.Commits)
		if err != nil {
			return UpdatePlan{}, fmt.Errorf("%s hook failed: %s: %w", e.name, name, err)
		}
		p.record(h.addon, agent.Finding{Event: e.name, Site: e.site, Finding: reply.Finding, Committed: committed})
		if reply.Plan == nil {
			continue
		}
		if plan, err = plan.merge(fromAgent(*reply.Plan)); err != nil {
			// merge's errors carry no identity to unwrap, so %v here would read the same.
			// mutator-disable-next-line expression/errorf-wrap
			return UpdatePlan{}, fmt.Errorf("%s hooks disagree: %w", e.name, err)
		}
	}
	return plan, nil
}

// Report adds every addon's report section to rec, as the agent builds it from what the addon's
// hooks found. A pipeline whose hooks found nothing new since the last call asks nothing.
func (p *Pipeline) Report(ctx context.Context, rec *report.Recorder) error {
	p.mu.Lock()
	changes := p.changes
	var request agent.ReportRequest
	for i, a := range p.addons {
		request.Addons = append(request.Addons, agent.AddonFindings{Addon: a.Name, Findings: slices.Clone(p.findings[i])})
	}
	p.mu.Unlock()
	if changes == p.reported {
		return nil
	}

	reply, err := p.agent.Report(ctx, request)
	if err != nil {
		return err
	}
	var unknown []string
	for name, section := range reply.Sections {
		if !slices.ContainsFunc(p.addons, func(a Addon) bool { return a.Name == name }) {
			unknown = append(unknown, name)
			continue
		}
		rec.AddSection(name, section)
	}
	if len(unknown) > 0 {
		slices.Sort(unknown)
		return fmt.Errorf("the agent reported sections of addons the run did not run: %v", unknown)
	}
	p.reported = changes
	return nil
}

// record keeps a hook's finding, replacing what the same hook found for the same site before.
func (p *Pipeline) record(addon int, finding agent.Finding) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.findings[addon] = slices.DeleteFunc(p.findings[addon], func(f agent.Finding) bool {
		return f.Event == finding.Event && f.Site == finding.Site
	})
	p.findings[addon] = append(p.findings[addon], finding)
	p.changes++
}

// earlier is what an addon's hooks found so far, by event and site; never another addon's.
func (p *Pipeline) earlier(addon int) []agent.Finding {
	p.mu.Lock()
	defer p.mu.Unlock()
	earlier := slices.Clone(p.findings[addon])
	slices.SortFunc(earlier, func(a, b agent.Finding) int {
		return cmp.Or(cmp.Compare(slices.Index(Events, a.Event), slices.Index(Events, b.Event)), cmp.Compare(a.Site, b.Site))
	})
	if earlier == nil {
		return []agent.Finding{}
	}
	return earlier
}

// addonHook is an addon's hook of one event, by the addon's index.
type addonHook struct {
	addon int
	stage Stage
}

// ordered sorts stably, so hooks sharing a stage keep addon order.
func (p *Pipeline) ordered(eventName string) []addonHook {
	var hooks []addonHook
	for i, a := range p.addons {
		if stage, ok := a.Hooks[eventName]; ok {
			hooks = append(hooks, addonHook{addon: i, stage: stage})
		}
	}
	slices.SortStableFunc(hooks, func(a, b addonHook) int { return cmp.Compare(a.stage, b.stage) })
	return hooks
}

// commitAll makes a hook's commits in order, returning the messages of those that were made.
func commitAll(ctx context.Context, committer Committer, commits []agent.Commit) ([]string, error) {
	committed := []string{}
	for _, c := range commits {
		made, err := committer.Commit(ctx, c.Message, c.Paths...)
		if err != nil {
			return nil, err
		}
		if made {
			committed = append(committed, c.Message)
		}
	}
	return committed, nil
}
