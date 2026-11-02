package workflow

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/drupdater/drupdater/internal/agent"
	"github.com/drupdater/drupdater/internal/repo"
	"github.com/drupdater/drupdater/internal/report"
)

// AddonAgent builds the addons' report sections: the Composer agent.
type AddonAgent interface {
	Report(ctx context.Context, request agent.ReportRequest) (agent.ReportReply, error)
}

// MergeRequestRenderer renders the merge request from the report: the Composer agent too.
type MergeRequestRenderer interface {
	MergeRequest(ctx context.Context, request agent.MergeRequestRequest) (agent.MergeRequestReply, error)
}

// App is drupdater's PHP app: it runs an event's hooks and makes the commits, its own and theirs.
type App interface {
	Fire(ctx context.Context, dir string, request agent.FireRequest) (agent.FireReply, error)
	Commit(ctx context.Context, dir string, request agent.CommitRequest) (agent.CommitReply, error)
}

// PipelineAgent is what a Pipeline calls: the app for the hooks, the agent for the report.
type PipelineAgent interface {
	App
	AddonAgent
}

// Pipeline fires a run's events at its addons through the app, and keeps what each hook found for
// its addon's later hooks and its report section.
type Pipeline struct {
	addons []Addon
	agent  PipelineAgent
	run    agent.Run

	// Per-site events fire concurrently; this lock is the only one between them.
	mu       sync.Mutex
	findings [][]agent.Finding
	// changes counts recorded findings, so Report asks the agent again only when there are new ones.
	changes  int
	reported int
}

// NewPipeline runs addons in this order within a stage: the catalogue's mandatory ones first.
func NewPipeline(addons []Addon, pipelineAgent PipelineAgent, security bool) *Pipeline {
	return &Pipeline{addons: addons, agent: pipelineAgent, run: agent.Run{Security: security}, findings: make([][]agent.Finding, len(addons))}
}

// fire runs every addon's hook for e in the project in dir, the hooks' commits by author. On
// pre-composer-update it returns e's plan merged with every hook's proposal. What the hooks found
// is kept even when one fails, so the report still has it.
func (p *Pipeline) fire(ctx context.Context, dir string, author repo.Identity, e event) (UpdatePlan, error) {
	// No hook, no app process.
	if !slices.ContainsFunc(p.addons, func(a Addon) bool { _, ok := a.Hooks[e.name]; return ok }) {
		return e.plan, nil
	}
	reply, err := p.agent.Fire(ctx, dir, agent.FireRequest{
		Event:  e.payload(),
		Addons: p.snapshot(),
		Run:    p.run,
		Author: agent.Author{Name: author.Name, Email: author.Email},
	})
	if err != nil {
		return UpdatePlan{}, fmt.Errorf("failed to fire %s: %w", e.name, err)
	}
	if err := p.record(reply.Recorded); err != nil {
		return UpdatePlan{}, err
	}
	switch {
	case reply.Failure != "":
		return UpdatePlan{}, errors.New(reply.Failure)
	case reply.Abort != "":
		return UpdatePlan{}, fmt.Errorf("%s hook failed: %w", e.name, AbortError{Msg: reply.Abort})
	case e.name != EventPreComposerUpdate:
		return UpdatePlan{}, nil
	case reply.Plan == nil:
		return UpdatePlan{}, fmt.Errorf("the app answered %s without a plan", e.name)
	}
	return fromAgent(*reply.Plan), nil
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

// snapshot is every addon as the app reads it, with what its hooks found so far.
func (p *Pipeline) snapshot() []agent.FireAddon {
	p.mu.Lock()
	defer p.mu.Unlock()
	addons := make([]agent.FireAddon, 0, len(p.addons))
	for i, a := range p.addons {
		findings := slices.Clone(p.findings[i])
		if findings == nil {
			findings = []agent.Finding{}
		}
		addons = append(addons, agent.FireAddon{Name: a.Name, Hooks: a.Hooks, Findings: findings})
	}
	return addons
}

// record keeps each hook's finding, replacing what the same hook found for the same site before.
func (p *Pipeline) record(recorded []agent.Recorded) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, r := range recorded {
		i := slices.IndexFunc(p.addons, func(a Addon) bool { return a.Name == r.Addon })
		if i < 0 {
			return fmt.Errorf("the app recorded a finding of %q, which the run does not run", r.Addon)
		}
		p.findings[i] = slices.DeleteFunc(p.findings[i], func(f agent.Finding) bool {
			return f.Event == r.Finding.Event && f.Site == r.Finding.Site
		})
		p.findings[i] = append(p.findings[i], r.Finding)
		p.changes++
	}
	return nil
}
