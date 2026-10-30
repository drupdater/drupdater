package composer

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	"go.uber.org/zap"
)

// Audit returns both halves of `composer audit` output: advisories and abandoned packages.
// --abandoned is not passed, so the project's own `audit.abandoned: ignore` still applies.
func (s *CLI) Audit(ctx context.Context, dir string) (Audit, error) {
	var composerAudit Audit
	out, err := s.execComposerJSON(ctx, dir, "audit", "--format=json", "--locked", "--no-plugins")
	if err != nil {
		// audit exits non-zero when it finds advisories; the JSON is still valid.
		s.logger.Debug("composer audit returned error", zap.Error(err))
	}

	if err := json.Unmarshal([]byte(out), &composerAudit); err != nil {
		return Audit{}, fmt.Errorf("failed to parse composer audit output: %w, output: %s", err, out)
	}

	return composerAudit, nil
}

type Source struct {
	Name     string `json:"name"`
	RemoteID string `json:"remoteId"`
}

type Advisory struct {
	ReportedAt       string   `json:"reportedAt"`
	Severity         string   `json:"severity"`
	AdvisoryID       string   `json:"advisoryId"`
	CVE              string   `json:"cve"`
	Sources          []Source `json:"sources"`
	Link             string   `json:"link"`
	PackageName      string   `json:"packageName"`
	AffectedVersions string   `json:"affectedVersions"`
	Title            string   `json:"title"`
}

// AdvisoriesMap is keyed by package name.
type AdvisoriesMap map[string]json.RawMessage

// AbandonedPackage has an empty Replacement when composer reported JSON null — no successor.
type AbandonedPackage struct {
	PackageName string `json:"packageName"`
	Replacement string `json:"replacement"`
}

type Audit struct {
	Advisories []Advisory         `json:"advisories"`
	Abandoned  []AbandonedPackage `json:"abandoned"`
}

// UnmarshalJSON flattens nested advisories and the abandoned object into sorted lists.
func (c *Audit) UnmarshalJSON(data []byte) error {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	advisories, err := flattenAdvisories(raw["advisories"])
	if err != nil {
		return err
	}

	c.Advisories = advisories
	c.Abandoned = flattenAbandoned(raw["abandoned"])
	return nil
}

// flattenAdvisories handles both shapes composer emits — a list, or a keyed map as drupal/core
// gets — since missing one silently drops an advisory from a security report.
func flattenAdvisories(advisoriesData any) ([]Advisory, error) {
	advMap, ok := advisoriesData.(map[string]any)
	// Equivalent mutant: ranging a nil map already yields (nil, nil). Kept to state the intent.
	// mutator-disable-next-line branch/if
	if !ok {
		return nil, nil
	}

	var advisories []Advisory
	for _, value := range advMap {
		var items []any
		switch v := value.(type) {
		case []any: // Simple advisory list
			items = v
		case map[string]any: // Nested map (e.g., drupal/core)
			items = slices.Collect(maps.Values(v))
		default:
			continue
		}

		for _, item := range items {
			var adv Advisory
			itemBytes, _ := json.Marshal(item)
			if err := json.Unmarshal(itemBytes, &adv); err != nil {
				return nil, err
			}
			advisories = append(advisories, adv)
		}
	}

	slices.SortFunc(advisories, compareAdvisories)

	return advisories, nil
}

// compareAdvisories orders the flattened list. Ranging a Go map is random, and this list reaches
// the run report, the merge request description and a security run's `composer update` arguments.
func compareAdvisories(a, b Advisory) int {
	return cmp.Or(
		strings.Compare(a.PackageName, b.PackageName),
		strings.Compare(a.AdvisoryID, b.AdvisoryID),
		strings.Compare(a.CVE, b.CVE),
		strings.Compare(a.Title, b.Title),
	)
}

// flattenAbandoned flattens composer audit's `abandoned` object into a sorted list. With nothing
// to report composer emits an empty JSON *array*, so a failed assertion must read as "none".
func flattenAbandoned(abandonedData any) []AbandonedPackage {
	entries, ok := abandonedData.(map[string]any)
	if !ok {
		return nil
	}

	abandoned := make([]AbandonedPackage, 0, len(entries))
	for name, replacement := range entries {
		pkg := AbandonedPackage{PackageName: name}
		// A non-string (null) means the maintainers named no successor.
		if suggestion, ok := replacement.(string); ok {
			pkg.Replacement = suggestion
		}
		abandoned = append(abandoned, pkg)
	}

	slices.SortFunc(abandoned, func(a, b AbandonedPackage) int {
		return strings.Compare(a.PackageName, b.PackageName)
	})

	return abandoned
}
