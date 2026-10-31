package external

import (
	"testing"

	"github.com/drupdater/drupdater/internal/workflow"
	"github.com/stretchr/testify/assert"
)

func build(Deps) workflow.Addon { return nil }

func TestValidateAcceptsASnakeCaseNameWithAConstructor(t *testing.T) {
	for _, name := range []string{"a", "acme", "acme_tags", "acme2", "a_1_b"} {
		assert.NoError(t, Registration{Name: name, New: build}.Validate(), name)
	}
}

func TestValidateRefusesANameAProjectCouldNotWriteAsOne(t *testing.T) {
	for _, name := range []string{"", "Acme", "1acme", "_acme", "acme-tags", "acme tags", "acme\n", "ácme"} {
		assert.ErrorContains(t, Registration{Name: name, New: build}.Validate(), "must be lowercase", "%q", name)
	}
}

func TestValidateRefusesARegistrationWithoutConstructor(t *testing.T) {
	assert.EqualError(t, Registration{Name: "acme"}.Validate(), `addon "acme" has no constructor`)
}
