package internal

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBasicAddon_Render(t *testing.T) {
	ba := &BasicAddon{}

	t.Run("renders a known template", func(t *testing.T) {
		out, err := ba.Render("update_hooks.go.tmpl", map[string]any{})
		require.NoError(t, err)
		assert.NotEmpty(t, out)
	})

	t.Run("unknown template returns error", func(t *testing.T) {
		_, err := ba.Render("nonexistent.go.tmpl", nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to execute template")
	})
}
