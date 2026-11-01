package composer

import (
	"context"
	"fmt"

	"github.com/drupdater/drupdater/internal/agent"
)

type Source struct {
	Name     string `json:"name"`
	RemoteID string `json:"remoteId"`
}

// Advisory's JSON names are the run report's, not the agent's: report.go publishes it as is.
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

// AbandonedPackage has an empty Replacement when the maintainers named no successor.
type AbandonedPackage struct {
	PackageName string `json:"packageName"`
	Replacement string `json:"replacement"`
}

// Audit is sorted: advisories by package, advisory ID, CVE and title; abandoned by package.
type Audit struct {
	Advisories []Advisory         `json:"advisories"`
	Abandoned  []AbandonedPackage `json:"abandoned"`
}

// A null in the agent's reply decodes to "", which is what Advisory and the report expect.
type auditResult struct {
	Advisories []struct {
		Package          string `json:"package"`
		AdvisoryID       string `json:"advisory_id"`
		Title            string `json:"title"`
		CVE              string `json:"cve"`
		Link             string `json:"link"`
		Severity         string `json:"severity"`
		AffectedVersions string `json:"affected_versions"`
		ReportedAt       string `json:"reported_at"`
		Sources          []struct {
			Name     string `json:"name"`
			RemoteID string `json:"remote_id"`
		} `json:"sources"`
	} `json:"advisories"`
	Abandoned []struct {
		Package     string `json:"package"`
		Replacement string `json:"replacement"`
	} `json:"abandoned"`
}

// Audit audits composer.lock through the agent's drupdater:audit, under the project's own audit
// policy, so its `audit.abandoned: ignore` and ignore lists still apply.
func (s *CLI) Audit(ctx context.Context, dir string) (Audit, error) {
	result, err := agent.Call[auditResult](ctx, s.command(dir), "drupdater:audit", struct{}{})
	if err != nil {
		return Audit{}, fmt.Errorf("failed to audit composer.lock: %w", err)
	}

	var audit Audit
	for _, a := range result.Advisories {
		advisory := Advisory{
			ReportedAt:       a.ReportedAt,
			Severity:         a.Severity,
			AdvisoryID:       a.AdvisoryID,
			CVE:              a.CVE,
			Link:             a.Link,
			PackageName:      a.Package,
			AffectedVersions: a.AffectedVersions,
			Title:            a.Title,
		}
		for _, source := range a.Sources {
			advisory.Sources = append(advisory.Sources, Source{Name: source.Name, RemoteID: source.RemoteID})
		}
		audit.Advisories = append(audit.Advisories, advisory)
	}
	for _, a := range result.Abandoned {
		audit.Abandoned = append(audit.Abandoned, AbandonedPackage{PackageName: a.Package, Replacement: a.Replacement})
	}
	return audit, nil
}
