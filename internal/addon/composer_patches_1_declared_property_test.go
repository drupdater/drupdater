package addon

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

func TestPropertyDeclaredPatchesRoundTripInOrder(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		packages := rapid.SliceOfDistinct(rapid.String(), rapid.ID).Draw(t, "packages")
		d := declaredPatches{packages: packages, sets: map[string][]patchEntry{}}
		for _, pkg := range packages {
			descriptions := rapid.SliceOfDistinct(rapid.String(), rapid.ID).Draw(t, "descriptions")
			set := []patchEntry{}
			for _, description := range descriptions {
				set = append(set, patchEntry{description: description, path: rapid.String().Draw(t, "path")})
			}
			d.sets[pkg] = set
		}

		encoded, err := json.Marshal(d)
		require.NoError(t, err)
		var back declaredPatches
		require.NoError(t, json.Unmarshal(encoded, &back))

		if len(packages) == 0 {
			assert.Empty(t, back.packages)
			return
		}
		assert.Equal(t, d, back)
	})
}
