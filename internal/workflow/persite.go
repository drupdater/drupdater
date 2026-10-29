package workflow

import (
	"maps"
	"sync"
)

// PerSite collects what a per-site hook finds, one value per site. Those hooks fire concurrently,
// so this is the only state they may share: each site sets its own entry under the lock, and
// readers get a copy, so no addon guards a map of its own.
type PerSite[T any] struct {
	mu     sync.Mutex
	values map[string]T
}

// Set records site's value, replacing an earlier one.
func (p *PerSite[T]) Set(site string, value T) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.values == nil {
		p.values = map[string]T{}
	}
	p.values[site] = value
}

// All returns every site's value, keyed by site. A copy: the caller may keep or change it.
func (p *PerSite[T]) All() map[string]T {
	p.mu.Lock()
	defer p.mu.Unlock()
	return maps.Clone(p.values)
}
