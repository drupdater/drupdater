package addon

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRender(t *testing.T) {
	t.Run("renders a known template", func(t *testing.T) {
		out, err := render("update_hooks.go.tmpl", map[string]any{})
		require.NoError(t, err)
		assert.NotEmpty(t, out)
	})

	t.Run("unknown template returns error", func(t *testing.T) {
		_, err := render("nonexistent.go.tmpl", nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to execute template")
	})
}
