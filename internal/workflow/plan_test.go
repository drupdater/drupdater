package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func pin(pkg, version string) Pin { return Pin{Package: pkg, Version: version} }

func TestMergeIntersectsRestrictions(t *testing.T) {
	merged, err := UpdatePlan{Only: []string{"drupal/core", "drupal/token"}}.merge(UpdatePlan{Only: []string{"drupal/webform", "drupal/token"}})
	require.NoError(t, err)
	assert.Equal(t, []string{"drupal/token"}, merged.Only)
}

func TestMergeKeepsTheOnlyRestriction(t *testing.T) {
	restricted := UpdatePlan{Only: []string{"drupal/token", "drupal/core"}}

	for name, merge := range map[string]func() (UpdatePlan, error){
		"restricted first":  func() (UpdatePlan, error) { return restricted.merge(UpdatePlan{}) },
		"restricted second": func() (UpdatePlan, error) { return UpdatePlan{}.merge(restricted) },
	} {
		t.Run(name, func(t *testing.T) {
			merged, err := merge()
			require.NoError(t, err)
			assert.Equal(t, []string{"drupal/core", "drupal/token"}, merged.Only, "sorted, but not widened")
		})
	}
}

func TestMergeDeduplicatesARestriction(t *testing.T) {
	merged, err := UpdatePlan{}.merge(UpdatePlan{Only: []string{"drupal/webform", "other/package", "drupal/webform"}})
	require.NoError(t, err)
	assert.Equal(t, []string{"drupal/webform", "other/package"}, merged.Only, "composer must not be handed a package twice")
}

func TestMergeRefusesDisjointRestrictions(t *testing.T) {
	_, err := UpdatePlan{Only: []string{"drupal/core"}}.merge(UpdatePlan{Only: []string{"drupal/webform", "drupal/token"}})
	require.EqualError(t, err, "the update is restricted to disjoint package sets: drupal/core and drupal/token, drupal/webform")
}

func TestMergeAccumulatesPins(t *testing.T) {
	merged, err := UpdatePlan{Keep: []Pin{pin("drupal/webform", "6.2.0"), pin("drupal/core", "10.2.0")}}.
		merge(UpdatePlan{Keep: []Pin{pin("drupal/core", "10.2.0"), pin("drupal/token", "1.13.0")}})
	require.NoError(t, err)
	assert.Equal(t, []Pin{pin("drupal/core", "10.2.0"), pin("drupal/token", "1.13.0"), pin("drupal/webform", "6.2.0")}, merged.Keep)
}

func TestMergeRefusesAPackagePinnedTwice(t *testing.T) {
	_, err := UpdatePlan{Keep: []Pin{pin("drupal/core", "10.3.0")}}.merge(UpdatePlan{Keep: []Pin{pin("drupal/core", "10.2.0")}})
	require.EqualError(t, err, "drupal/core is pinned at both 10.2.0 and 10.3.0")
}

func TestMergeAsksForMinimalChangesIfEitherDoes(t *testing.T) {
	for _, tc := range []struct{ a, b, want bool }{
		{false, false, false},
		{true, false, true},
		{false, true, true},
		{true, true, true},
	} {
		merged, err := UpdatePlan{MinimalChanges: tc.a}.merge(UpdatePlan{MinimalChanges: tc.b})
		require.NoError(t, err)
		assert.Equal(t, tc.want, merged.MinimalChanges, "%t merged with %t", tc.a, tc.b)
	}
}

func TestMergingNothingLeavesNothing(t *testing.T) {
	merged, err := UpdatePlan{}.merge(UpdatePlan{Only: []string{}, Keep: []Pin{}})
	require.NoError(t, err)
	assert.Zero(t, merged)
}

func TestKeepConstraints(t *testing.T) {
	assert.Nil(t, UpdatePlan{}.KeepConstraints())
	assert.Equal(t, []string{"drupal/core:10.2.0", "drupal/foo:1.x-dev 0424dff"},
		UpdatePlan{Keep: []Pin{pin("drupal/core", "10.2.0"), pin("drupal/foo", "1.x-dev 0424dff")}}.KeepConstraints())
}
