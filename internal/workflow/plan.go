package workflow

import (
	"slices"

	"github.com/drupdater/drupdater/internal/agent"
)

// UpdatePlan is what `composer update` is asked to do, as the app merged the addons' proposals.
type UpdatePlan struct {
	// Only restricts the update to these packages; empty means every package.
	Only []string
	// Keep holds packages at a version, whatever else the update does.
	Keep []Pin
	// MinimalChanges asks composer to disturb as little else as possible.
	MinimalChanges bool
}

// Pin holds Package at Version.
type Pin struct {
	Package string
	Version string
}

// String is the form composer's --with takes.
func (p Pin) String() string {
	return p.Package + ":" + p.Version
}

// KeepConstraints is Keep in the "package:version" form composer's --with takes.
func (p UpdatePlan) KeepConstraints() []string {
	var constraints []string
	for _, pin := range p.Keep {
		constraints = append(constraints, pin.String())
	}
	return constraints
}

// toAgent is the plan as the app reads it, every list a list.
func (p UpdatePlan) toAgent() *agent.Plan {
	plan := &agent.Plan{Only: slices.Clone(p.Only), Keep: []agent.Pin{}, MinimalChanges: p.MinimalChanges}
	if plan.Only == nil {
		plan.Only = []string{}
	}
	for _, pin := range p.Keep {
		plan.Keep = append(plan.Keep, agent.Pin{Package: pin.Package, Version: pin.Version})
	}
	return plan
}

// fromAgent is the plan the app merged.
func fromAgent(merged agent.Plan) UpdatePlan {
	plan := UpdatePlan{Only: slices.Clone(merged.Only), MinimalChanges: merged.MinimalChanges}
	for _, pin := range merged.Keep {
		plan.Keep = append(plan.Keep, Pin{Package: pin.Package, Version: pin.Version})
	}
	return plan
}
