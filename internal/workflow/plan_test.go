package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/drupdater/drupdater/internal/agent"
)

func TestKeepConstraints(t *testing.T) {
	assert.Nil(t, UpdatePlan{}.KeepConstraints())
	assert.Equal(t, []string{"drupal/core:10.2.0", "drupal/foo:1.x-dev 0424dff"},
		UpdatePlan{Keep: []Pin{{Package: "drupal/core", Version: "10.2.0"}, {Package: "drupal/foo", Version: "1.x-dev 0424dff"}}}.KeepConstraints())
}

func TestThePlanRoundTripsThroughTheAppsShape(t *testing.T) {
	plan := UpdatePlan{Only: []string{"a/b"}, Keep: []Pin{{Package: "c/d", Version: "1.0.0"}}, MinimalChanges: true}

	assert.Equal(t, plan, fromAgent(*plan.toAgent()))
	assert.Equal(t, &agent.Plan{Only: []string{}, Keep: []agent.Pin{}}, UpdatePlan{}.toAgent(), "every list a list, never null")
}
