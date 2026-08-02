package cmd

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/drupdater/drupdater/internal"
	"github.com/drupdater/drupdater/internal/logging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

// COMPOSER_AUTH is a JSON blob of credentials whose shape Composer defines and extends, so the
// walk that finds the secrets in it has to hold for structures nobody here wrote down. The
// properties end where it matters: the value that reaches the log.

// authLeafGen generates one credential value.
func authLeafGen() *rapid.Generator[string] {
	return rapid.StringMatching(`[a-zA-Z0-9_-]{8,24}`)
}

// authTreeGen builds a COMPOSER_AUTH-shaped JSON tree and reports, alongside it, exactly which
// of its string leaves are secrets. key is the object key the value was reached through.
//
// The bookkeeping states the documented rule rather than mirroring the walk: a string is a
// secret unless it was reached through a "username" key — Composer's http-basic form commonly
// sets that to the literal word "token", and redacting it would black out unrelated log output.
// An array inherits the key it sits under, an object gives each of its values its own, and
// numbers and booleans are never secrets. Encoding the rule here is what lets the property
// generate nesting nobody wrote down and still know the right answer.
func authTreeGen(depth int, key string) *rapid.Generator[authTree] {
	leafGen := func(t *rapid.T) authTree {
		leaf := authLeafGen().Draw(t, "leaf")
		if key == "username" {
			return authTree{value: leaf}
		}
		return authTree{value: leaf, secrets: []string{leaf}}
	}

	return rapid.Custom(func(t *rapid.T) authTree {
		if depth == 0 {
			return leafGen(t)
		}

		switch rapid.SampledFrom([]string{"leaf", "number", "bool", "object", "array"}).Draw(t, "kind") {
		case "number":
			return authTree{value: float64(rapid.IntRange(0, 1000).Draw(t, "number"))}
		case "bool":
			return authTree{value: rapid.Bool().Draw(t, "bool")}
		case "array":
			items := rapid.SliceOfN(authTreeGen(depth-1, key), 0, 3).Draw(t, "items")
			value := make([]any, 0, len(items))
			var secrets []string
			for _, item := range items {
				value = append(value, item.value)
				secrets = append(secrets, item.secrets...)
			}
			return authTree{value: value, secrets: secrets}
		case "object":
			keys := rapid.SliceOfNDistinct(
				rapid.SampledFrom([]string{"http-basic", "bearer", "github-oauth", "gitlab-token", "username", "password", "example.com"}),
				0, 4, rapid.ID,
			).Draw(t, "keys")
			value := map[string]any{}
			var secrets []string
			for _, childKey := range keys {
				child := authTreeGen(depth-1, childKey).Draw(t, "child-"+childKey)
				value[childKey] = child.value
				secrets = append(secrets, child.secrets...)
			}
			return authTree{value: value, secrets: secrets}
		default:
			return leafGen(t)
		}
	})
}

// authTree is a generated COMPOSER_AUTH value together with the leaves that must be redacted.
type authTree struct {
	value   any
	secrets []string
}

func TestPropertyComposerAuthSecretLeavesFindsEveryCredential(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		tree := authTreeGen(3, "").Draw(t, "tree")

		// Compared as a set: the walk iterates maps, which has no defined order.
		assert.ElementsMatch(t, tree.secrets, composerAuthSecretLeaves(tree.value, ""))
	})
}

func TestPropertyComposerAuthSecretLeavesIgnoresNesting(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		tree := authTreeGen(2, "").Draw(t, "tree")
		wraps := rapid.IntRange(1, 4).Draw(t, "wraps")

		// An array propagates the key it was reached through, so wrapping a value in any number
		// of arrays must not change which of its leaves count as secrets. Composer nests its
		// per-host blocks differently between auth types, and the walk has to be indifferent
		// to how deep a credential sits.
		wrapped := tree.value
		for range wraps {
			wrapped = []any{wrapped}
		}

		assert.ElementsMatch(t, composerAuthSecretLeaves(tree.value, ""), composerAuthSecretLeaves(wrapped, ""))
	})
}

func TestPropertyRegisterComposerAuthRedactsEveryCredential(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		tree := authTreeGen(3, "").Draw(t, "tree")

		encoded, err := json.Marshal(tree.value)
		require.NoError(t, err)

		redactor := logging.NewRedactor()
		registerComposerAuth(redactor, string(encoded))

		// The promise that matters, stated across the two packages that make it: whatever
		// Composer echoes back of the credentials it was handed, the redactor knows the value.
		for _, secret := range tree.secrets {
			assert.Equal(t, "***", redactor.Redact(secret))
			assert.NotContains(t, redactor.Redact("fetch failed for https://"+secret+"@example.com/x.git"), secret)
		}
	})
}

func TestPropertyRegisterComposerAuthFallsBackToTheRawValue(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Not JSON at all: an operator can put anything in the variable, and a value that fails
		// to parse still has to be kept out of the log.
		raw := authLeafGen().Draw(t, "raw")

		redactor := logging.NewRedactor()
		registerComposerAuth(redactor, raw)

		assert.Equal(t, "***", redactor.Redact(raw))
	})
}

func TestPropertyConfigurableAddonsMatchesTheRegistry(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		names := configurableAddons()

		// The list `drupdater addons` prints is what a user may write in .drupdater.yaml, so it
		// has to be exactly the registry minus the addons that always run. Adding a registry
		// entry without touching this function would leave the command silent about it.
		want := make([]string, 0, len(addonRegistry))
		for name := range addonRegistry {
			if name != "composer_audit" && !slices.Contains(mandatoryAddons, name) {
				want = append(want, name)
			}
		}
		assert.ElementsMatch(t, want, names)
		assert.True(t, slices.IsSorted(names), "the printed list is sorted")
		assert.Len(t, slices.Compact(slices.Clone(names)), len(names), "and free of duplicates")

		// Round-trip: every name it offers is a name validateAddons accepts, under either run
		// type, since a typo in one block aborts a run in the other too.
		split := rapid.IntRange(0, len(names)).Draw(t, "split")
		assert.NoError(t, validateAddons(internal.Config{RunTypes: internal.RunTypesConfig{
			Normal:   internal.RunTypeConfig{Addons: names[:split]},
			Security: internal.RunTypeConfig{Addons: names[split:]},
		}}))
	})
}

func TestPropertyValidateAddonsRejectsUnknownNamesInEitherRunType(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		unknown := rapid.StringMatching(`[a-z_]{1,20}`).
			Filter(func(s string) bool { _, ok := addonRegistry[s]; return !ok }).
			Draw(t, "unknown")
		known := rapid.SliceOfNDistinct(rapid.SampledFrom(configurableAddons()), 0, 3, rapid.ID).Draw(t, "known")
		inSecurity := rapid.Bool().Draw(t, "inSecurity")

		config := internal.Config{RunTypes: internal.RunTypesConfig{
			Normal:   internal.RunTypeConfig{Addons: known},
			Security: internal.RunTypeConfig{Addons: known},
		}}
		if inSecurity {
			config.RunTypes.Security.Addons = append(slices.Clone(known), unknown)
		} else {
			config.RunTypes.Normal.Addons = append(slices.Clone(known), unknown)
		}

		// Symmetric on purpose: a typo under run_types.security is caught on a normal run and
		// the other way round, so the mistake surfaces the first time the tool runs at all
		// rather than the first time a security update happens to be due.
		err := validateAddons(config)
		require.Error(t, err)
		assert.Contains(t, err.Error(), unknown)
	})
}
