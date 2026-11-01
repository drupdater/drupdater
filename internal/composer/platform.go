package composer

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/drupdater/drupdater/internal/agent"
)

type platformCheckResult struct {
	Unmet []struct {
		Name string `json:"name"`
		// Version is empty when the platform package is missing altogether.
		Version    string `json:"version"`
		Constraint string `json:"constraint"`
		RequiredBy string `json:"required_by"`
	} `json:"unmet"`
}

// CheckPlatformReqs verifies the PHP version satisfies composer.lock, and is the only fail-fast
// check because `Update` runs with --ignore-platform-reqs. ext-* is ignored: the operator cannot
// act on which extensions drupdater's own runtime loaded. On failure, the string lists each
// unmet requirement, one per line.
func (s *CLI) CheckPlatformReqs(ctx context.Context, dir string) (string, error) {
	result, err := agent.Call[platformCheckResult](ctx, s.command(dir), "drupdater:platform-check", struct{}{})
	if err != nil {
		return err.Error(), fmt.Errorf("failed to check platform requirements: %w", err)
	}

	var failed []string
	for _, req := range result.Unmet {
		if strings.HasPrefix(req.Name, "ext-") {
			continue
		}
		found := req.Version
		if found == "" {
			found = "nothing"
		}
		failed = append(failed, fmt.Sprintf("%s: %s requires %s, found %s", req.Name, req.RequiredBy, req.Constraint, found))
	}

	if len(failed) > 0 {
		return strings.Join(failed, "\n"), errors.New("unmet platform requirements")
	}
	return "", nil
}
