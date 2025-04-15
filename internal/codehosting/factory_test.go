package codehosting

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDefaultVcsProviderFactory_Create(t *testing.T) {
	factory := NewDefaultVcsProviderFactory()

	t.Run("returns gitlab platform", func(t *testing.T) {
		provider := factory.Create("https://gitlab.com", "dummy-token")
		assert.IsType(t, &Gitlab{}, provider)
	})

	t.Run("returns nil for unknown provider", func(t *testing.T) {

		factory := NewDefaultVcsProviderFactory()
		provider := factory.Create("https://gitfoo.com", "dummy-token")
		assert.IsType(t, &Gitlab{}, provider)
	})
}
