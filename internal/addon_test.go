package internal

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBasicAddonRender(t *testing.T) {
	var ba BasicAddon

	t.Run("renders an embedded addon template", func(t *testing.T) {
		out, err := ba.Render("composer_diff.go.tmpl", "| package | from | to |")
		require.NoError(t, err)
		assert.Contains(t, out, "Dependency updates")
		assert.Contains(t, out, "| package | from | to |")
	})

	t.Run("escapes pipes and newlines in table cells", func(t *testing.T) {
		// The cell helper keeps a value containing "|" or a newline from breaking the markdown
		// table it is interpolated into.
		out, err := ba.Render("unsupported_modules.go.tmpl", []struct {
			Name               string
			InstalledVersion   string
			RecommendedVersion string
		}{
			{Name: "a|b\nc", InstalledVersion: "1.0", RecommendedVersion: "None"},
		})
		require.NoError(t, err)
		assert.Contains(t, out, `a\|b c`)
		assert.NotContains(t, out, "a|b")
	})

	t.Run("errors for an unknown template name", func(t *testing.T) {
		_, err := ba.Render("does_not_exist.go.tmpl", nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to execute template")
	})

	t.Run("errors when the template cannot be executed with the given data", func(t *testing.T) {
		// update_hooks.go.tmpl ranges over a map of sites; a string has no such shape.
		_, err := ba.Render("update_hooks.go.tmpl", "not-a-map")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to execute template")
	})
}

func TestCellReplacer(t *testing.T) {
	assert.Equal(t, `a\|b`, cellReplacer.Replace("a|b"))
	assert.Equal(t, "a b", cellReplacer.Replace("a\nb"))
	assert.Equal(t, "ab", cellReplacer.Replace("a\rb"))
	assert.Equal(t, "plain", cellReplacer.Replace("plain"))
}
