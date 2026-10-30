package composer

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// platformRequirementConstraint is the unmet constraint of a failed or missing row.
type platformRequirementConstraint struct {
	Constraint string `json:"constraint"`
}

// platformRequirement is one row of `composer check-platform-reqs --format=json` output.
// Status is "success", "failed" (version constraint mismatch), or "missing" (package absent).
type platformRequirement struct {
	Name              string                         `json:"name"`
	Version           string                         `json:"version"`
	Status            string                         `json:"status"`
	FailedRequirement *platformRequirementConstraint `json:"failed_requirement"`
}

// CheckPlatformReqs verifies the PHP version satisfies composer.lock, and is the only fail-fast
// check because `Update` runs with --ignore-platform-reqs. ext-* is ignored: the operator cannot
// act on which extensions drupdater's own runtime loaded.
func (s *CLI) CheckPlatformReqs(ctx context.Context, dir string) (string, error) {
	out, err := s.execComposer(ctx, dir, "check-platform-reqs", "--lock", "--no-ansi", "--format=json")

	// composer prints an informational line ahead of the payload, so extract just the array.
	jsonPayload := out
	if start, end := strings.Index(out, "["), strings.LastIndex(out, "]"); start != -1 && end > start {
		jsonPayload = out[start : end+1]
	}

	var requirements []platformRequirement
	if jsonErr := json.Unmarshal([]byte(jsonPayload), &requirements); jsonErr != nil {
		if err != nil {
			return out, err
		}
		return out, fmt.Errorf("failed to parse composer check-platform-reqs output: %w, output: %s", jsonErr, out)
	}

	var failed []string
	for _, req := range requirements {
		if strings.HasPrefix(req.Name, "ext-") {
			continue
		}
		if req.Status == "success" {
			continue
		}
		required := "?"
		if req.FailedRequirement != nil {
			required = req.FailedRequirement.Constraint
		}
		failed = append(failed, fmt.Sprintf("%s: required %s, found %s (%s)", req.Name, required, req.Version, req.Status))
	}

	if len(failed) > 0 {
		return strings.Join(failed, "\n"), fmt.Errorf("unmet platform requirements")
	}

	return "", nil
}
