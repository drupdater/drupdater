package workflow

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
)

// UpdatePlan is what `composer update` is asked to do. A pre-composer-update hook reads the plan
// earlier stages settled and returns its own proposal; the pipeline merges the proposals.
type UpdatePlan struct {
	// Only restricts the update to these packages; empty means every package.
	Only []string
	// Keep holds packages at a version, whatever else the update does.
	Keep []Pin
	// MinimalChanges asks composer to disturb as little else as possible.
	MinimalChanges bool
}

// Pin holds Package at Version.
type Pin struct {
	Package string
	Version string
}

// String is the form composer's --with takes.
func (p Pin) String() string {
	return p.Package + ":" + p.Version
}

// KeepConstraints is Keep in the "package:version" form composer's --with takes.
func (p UpdatePlan) KeepConstraints() []string {
	var constraints []string
	for _, pin := range p.Keep {
		constraints = append(constraints, pin.String())
	}
	return constraints
}

// merge is commutative and associative, so the order addons propose in never changes the plan:
// restrictions intersect, pins accumulate, and any proposal can ask for minimal changes.
func (p UpdatePlan) merge(q UpdatePlan) (UpdatePlan, error) {
	only, err := intersectRestrictions(p.Only, q.Only)
	if err != nil {
		return UpdatePlan{}, err
	}
	keep, err := unionPins(p.Keep, q.Keep)
	if err != nil {
		return UpdatePlan{}, err
	}
	return UpdatePlan{Only: only, Keep: keep, MinimalChanges: p.MinimalChanges || q.MinimalChanges}, nil
}

// intersectRestrictions refuses an empty intersection: passed on, it would read as "update
// everything", the opposite of what both proposals asked for.
func intersectRestrictions(a, b []string) ([]string, error) {
	a, b = sortedSet(a), sortedSet(b)
	if len(a) == 0 {
		return b, nil
	}
	if len(b) == 0 {
		return a, nil
	}
	both := slices.DeleteFunc(slices.Clone(a), func(p string) bool {
		_, found := slices.BinarySearch(b, p)
		return !found
	})
	if len(both) == 0 {
		return nil, fmt.Errorf("the update is restricted to disjoint package sets: %s and %s",
			strings.Join(a, ", "), strings.Join(b, ", "))
	}
	return both, nil
}

func unionPins(a, b []Pin) ([]Pin, error) {
	pins := slices.Concat(a, b)
	slices.SortFunc(pins, func(x, y Pin) int {
		return cmp.Or(strings.Compare(x.Package, y.Package), strings.Compare(x.Version, y.Version))
	})
	pins = slices.Compact(pins)
	for i := 1; i < len(pins); i++ {
		if pins[i].Package == pins[i-1].Package {
			return nil, fmt.Errorf("%s is pinned at both %s and %s", pins[i].Package, pins[i-1].Version, pins[i].Version)
		}
	}
	return pins, nil
}

func sortedSet(s []string) []string {
	return slices.Compact(slices.Sorted(slices.Values(s)))
}

// clone keeps a hook from editing the plan its stage-mates read through their own Plan().
func (p UpdatePlan) clone() UpdatePlan {
	return UpdatePlan{Only: slices.Clone(p.Only), Keep: slices.Clone(p.Keep), MinimalChanges: p.MinimalChanges}
}
