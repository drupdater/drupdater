package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

// Small pools, so restrictions overlap, pins collide and both kinds of disagreement occur often.
var (
	planPackages = []string{"drupal/core", "drupal/token", "drupal/webform", "other/package"}
	planVersions = []string{"1.0.0", "2.0.0"}
)

func planGen() *rapid.Generator[UpdatePlan] {
	return rapid.Custom(func(t *rapid.T) UpdatePlan {
		return UpdatePlan{
			Only: rapid.SliceOfN(rapid.SampledFrom(planPackages), 0, 4).Draw(t, "only"),
			Keep: rapid.SliceOfN(rapid.Custom(func(t *rapid.T) Pin {
				return Pin{
					Package: rapid.SampledFrom(planPackages).Draw(t, "package"),
					Version: rapid.SampledFrom(planVersions).Draw(t, "version"),
				}
			}), 0, 3).Draw(t, "keep"),
			MinimalChanges: rapid.Bool().Draw(t, "minimal"),
		}
	})
}

// mergeAll folds plans left to right, as the pipeline does.
func mergeAll(plans ...UpdatePlan) (UpdatePlan, error) {
	var merged UpdatePlan
	for _, p := range plans {
		var err error
		if merged, err = merged.merge(p); err != nil {
			return UpdatePlan{}, err
		}
	}
	return merged, nil
}

func TestPropertyMergeIsCommutative(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		a, b := planGen().Draw(t, "a"), planGen().Draw(t, "b")

		ab, errAB := a.merge(b)
		ba, errBA := b.merge(a)

		require.Equal(t, errAB == nil, errBA == nil, "one order fails and the other does not: %v / %v", errAB, errBA)
		assert.Equal(t, ab, ba)
	})
}

func TestPropertyMergeIsAssociative(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		a, b, c := planGen().Draw(t, "a"), planGen().Draw(t, "b"), planGen().Draw(t, "c")

		left, errLeft := mergeAll(a, b, c)
		right, errRight := b.merge(c)
		if errRight == nil {
			right, errRight = a.merge(right)
		}

		require.Equal(t, errLeft == nil, errRight == nil, "grouping decides whether the merge fails: %v / %v", errLeft, errRight)
		if errLeft == nil {
			assert.Equal(t, left, right)
		}
	})
}

func TestPropertyMergingAPlanWithItselfChangesNothing(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		settled, err := mergeAll(planGen().Draw(t, "plan"))
		if err != nil {
			t.Skip("a plan pinning one package twice never settles")
		}

		again, err := settled.merge(settled)
		require.NoError(t, err)
		assert.Equal(t, settled, again)
	})
}

func TestPropertyMergeNeverWidensARestriction(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		a, b := planGen().Draw(t, "a"), planGen().Draw(t, "b")

		merged, err := a.merge(b)
		if err != nil {
			return
		}
		for _, side := range []UpdatePlan{a, b} {
			if len(side.Only) == 0 {
				continue
			}
			require.NotEmpty(t, merged.Only, "a restricted proposal must never turn into \"update everything\"")
			for _, p := range merged.Only {
				assert.Contains(t, side.Only, p)
			}
		}
	})
}
