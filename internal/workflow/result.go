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

// Sites returns every site's finding for event. It panics when a finding is not an F, which only
// an addon reading a different type than its hook returns can cause.
func Sites[F any](f *Findings, event string) map[string]Recorded[F] {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]Recorded[F]{}
	for key, r := range f.recorded {
		if key.event != event || key.site == "" {
			continue
		}
		finding, ok := r.Finding.(F)
		if !ok {
			panic(fmt.Sprintf("%s finding for site %s is a %T, not a %T", event, key.site, r.Finding, finding))
		}
		out[key.site] = Recorded[F]{Finding: finding, Committed: slices.Clone(r.Committed)}
	}
	return out
}

// Reporter is report.Reporter for an addon whose hooks return their findings: its section is
// built from what the pipeline recorded, not from state the addon kept.
type Reporter interface {
	ReportKey() string
	ReportData(findings *Findings) any
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
	Commit(string, ...string) (bool, error)
}, commits []Commit) ([]string, error) {
	var committed []string
	for _, c := range commits {
		made, err := e.Commit(c.Message, c.Paths...)
		if err != nil {
			return nil, err
		}
		if made {
			committed = append(committed, c.Message)
		}
	}
	return committed, nil
}
