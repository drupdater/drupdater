package composer

import (
	"context"
	"fmt"
	"strings"

	"github.com/drupdater/drupdater/internal/agent"
)

type PackageChange struct {
	Action  string // Install, Upgrade, Remove, Downgrade
	Package string
	From    string
	To      string
}

type resolveRequest struct {
	Packages       []string     `json:"packages"`
	Keep           []resolvePin `json:"keep"`
	MinimalChanges bool         `json:"minimal_changes"`
}

type resolvePin struct {
	Package string `json:"package"`
	Version string `json:"version"`
}

type resolveResult struct {
	Changes []struct {
		Action  string `json:"action"`
		Package string `json:"package"`
		From    string `json:"from"`
		To      string `json:"to"`
	} `json:"changes"`
}

// Update resolves into composer.json and composer.lock only, through the agent's drupdater:resolve;
// Install brings vendor/ to the result.
//
// packagesToKeep are "package:version" pins, the version as the lock prints it. The changes come
// sorted by action, Upgrade, Downgrade, Remove, Install, then by package.
func (s *CLI) Update(ctx context.Context, dir string, packages []string, packagesToKeep []string, minimalChanges bool) ([]PackageChange, error) {
	request := resolveRequest{Packages: packages, Keep: []resolvePin{}, MinimalChanges: minimalChanges}
	if request.Packages == nil {
		request.Packages = []string{}
	}
	for _, pin := range packagesToKeep {
		name, version, _ := strings.Cut(pin, ":")
		request.Keep = append(request.Keep, resolvePin{Package: name, Version: version})
	}

	result, err := agent.Call[resolveResult](ctx, s.command(dir), "drupdater:resolve", request)
	if err != nil {
		return nil, fmt.Errorf("failed to update dependencies: %w", err)
	}

	var changes []PackageChange
	for _, c := range result.Changes {
		changes = append(changes, PackageChange{Action: c.Action, Package: c.Package, From: c.From, To: c.To})
	}
	return changes, nil
}
