package workflow

import (
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPerSiteZeroValueIsEmpty(t *testing.T) {
	var p PerSite[int]

	assert.Empty(t, p.All())
}

func TestPerSiteKeepsTheLastValueOfEachSite(t *testing.T) {
	var p PerSite[string]

	p.Set("default", "first")
	p.Set("second", "other")
	p.Set("default", "again")

	assert.Equal(t, map[string]string{"default": "again", "second": "other"}, p.All())
}

// A reader may change what All returned without reaching what the sites recorded.
func TestPerSiteAllIsACopy(t *testing.T) {
	var p PerSite[string]
	p.Set("default", "recorded")

	all := p.All()
	all["default"] = "changed"
	all["injected"] = "x"

	assert.Equal(t, map[string]string{"default": "recorded"}, p.All())
}

// What the per-site hooks rely on, and what -race checks: every site writes at once, none is lost.
func TestPerSiteUnderConcurrentSites(t *testing.T) {
	var p PerSite[int]
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range 32 {
		wg.Go(func() {
			<-start
			p.Set("site"+strconv.Itoa(i), i)
			_ = p.All()
		})
	}
	close(start)
	wg.Wait()

	all := p.All()
	assert.Len(t, all, 32)
	assert.Equal(t, 7, all["site7"])
}
