package workflow

import (
	"fmt"
	"slices"
	"sync"
)

// Commit is one commit a hook asks for. The pipeline makes it before the next hook runs, so a
// later stage sees it as committed.
type Commit struct {
	Message string
	Paths   []string
}

// Result is what a hook built by Produce returns, in place of committing itself or storing anything.
type Result[F any] struct {
	Commits []Commit
	Finding F
}

// Proposal is what a hook built by ProduceProposal returns: a Result with its plan proposal.
type Proposal[F any] struct {
	Plan    UpdatePlan
	Commits []Commit
	Finding F
}

// Recorded is one hook's finding, with the messages of those of its Commits that made a commit.
type Recorded[F any] struct {
	Finding   F
	Committed []string
}

type findingKey struct {
	event string
	site  string
}

// Findings is what one addon's hooks returned over a run. Per-site hooks fire concurrently, so
// this lock is the only one between them; the addons hold no state of their own.
type Findings struct {
	mu       sync.Mutex
	recorded map[findingKey]Recorded[any]
}

// Record keeps a hook's finding for event and site, which is "" for an event not per site.
func (f *Findings) Record(event string, site string, finding any, committed []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.recorded == nil {
		f.recorded = map[findingKey]Recorded[any]{}
	}
	f.recorded[findingKey{event: event, site: site}] = Recorded[any]{Finding: finding, Committed: slices.Clone(committed)}
}

// Sites returns every site's finding for event. It panics when a finding is not an F.
func Sites[F any](f *Findings, event string) map[string]Recorded[F] {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]Recorded[F]{}
	for key, r := range f.recorded {
		if key.event != event || key.site == "" {
			continue
		}
		out[key.site] = typed[F](r, event+" finding for site "+key.site)
	}
	return out
}

// Shared returns event's finding for an event not per site, and whether its hook recorded one.
func Shared[F any](f *Findings, event string) (Recorded[F], bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.recorded[findingKey{event: event}]
	if !ok {
		return Recorded[F]{}, false
	}
	return typed[F](r, event+" finding"), true
}

// typed panics when the finding is not an F, which only an addon reading a different type than
// its hook returns can cause.
func typed[F any](r Recorded[any], what string) Recorded[F] {
	finding, ok := r.Finding.(F)
	if !ok {
		panic(fmt.Sprintf("%s is a %T, not a %T", what, r.Finding, finding))
	}
	return Recorded[F]{Finding: finding, Committed: slices.Clone(r.Committed)}
}

// Reporter is an addon contributing a section to the run report, built from what the pipeline
// recorded of its hooks rather than from state the addon kept. Key it by the addon's name.
type Reporter interface {
	ReportKey() string
	ReportData(findings *Findings) any
}

// Describer is a Reporter that also adds a section to the merge request description: a
// text/template rendered against its own report section, read back from the report's JSON.
// The builtin addons' sections are rendered by internal/mergerequest instead.
type Describer interface {
	Reporter
	DescriptionTemplate() string
}

// SiteFindings returns just the findings of Sites, for an addon that does not report commits.
func SiteFindings[F any](f *Findings, event string) map[string]F {
	out := map[string]F{}
	for site, r := range Sites[F](f, event) {
		out[site] = r.Finding
	}
	return out
}

func commitAll(e interface {
	commit(string, ...string) (bool, error)
}, commits []Commit) ([]string, error) {
	var committed []string
	for _, c := range commits {
		made, err := e.commit(c.Message, c.Paths...)
		if err != nil {
			return nil, err
		}
		if made {
			committed = append(committed, c.Message)
		}
	}
	return committed, nil
}
