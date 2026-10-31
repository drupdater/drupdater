package addon

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/drupdater/drupdater/internal/composer"
	"github.com/drupdater/drupdater/internal/workflow"

	"go.uber.org/zap"
)

// AuditComposer runs composer audit.
type AuditComposer interface {
	Audit(ctx context.Context, dir string) (composer.Audit, error)
}

// ComposerAudit runs on every update: a routine update that closes a CVE should say so. Only a
// security run lets the audit dictate the update's scope, and abort when there is nothing to fix.
type ComposerAudit struct {
	logger   *zap.Logger
	composer AuditComposer
	security bool
}

// NewComposerAudit creates a security auditor. security marks a `--security` run.
func NewComposerAudit(logger *zap.Logger, composer AuditComposer, security bool) *ComposerAudit {
	return &ComposerAudit{
		logger:   logger,
		composer: composer,
		security: security,
	}
}

func (ca *ComposerAudit) Hooks() workflow.Hooks {
	return workflow.Hooks{
		PreComposerUpdate: workflow.ProduceProposal(workflow.Prepare, ca.preComposerUpdateHandler),
		PostCodeUpdate:    workflow.ProduceFrom(workflow.Inspect, ca.postCodeUpdateHandler),
	}
}

// preComposerUpdateHandler audits the pre-update code. A security run narrows the update to the
// affected packages and aborts when there are none; a normal run only records the audit.
func (ca *ComposerAudit) preComposerUpdateHandler(evt *workflow.PreComposerUpdateEvent) (workflow.Proposal[composer.Audit], error) {
	before, err := ca.composer.Audit(evt.Context(), evt.Path())
	if err != nil {
		return workflow.Proposal[composer.Audit]{}, fmt.Errorf("failed to run composer audit: %w", err)
	}

	proposal := workflow.Proposal[composer.Audit]{Finding: before}
	if !ca.security {
		return proposal, nil
	}

	// Several advisories often name one package; merging the plan deduplicates them.
	packagesToUpdate := make([]string, 0, len(before.Advisories))
	for _, advisory := range before.Advisories {
		packagesToUpdate = append(packagesToUpdate, advisory.PackageName)
	}

	if slices.Contains(packagesToUpdate, "drupal/core") {
		packagesToUpdate = append(packagesToUpdate, "drupal/core-recommended")
		packagesToUpdate = append(packagesToUpdate, "drupal/core-composer-scaffold")
	}

	if len(packagesToUpdate) == 0 {
		return workflow.Proposal[composer.Audit]{}, workflow.AbortError{Msg: "No security advisories found"}
	}

	proposal.Plan = workflow.UpdatePlan{Only: packagesToUpdate, MinimalChanges: true}
	return proposal, nil
}

// postCodeUpdateHandler audits the updated code, against the audit taken before the update.
func (ca *ComposerAudit) postCodeUpdateHandler(evt *workflow.PostCodeUpdateEvent, earlier *workflow.Findings) (workflow.Result[composer.Audit], error) {
	after, err := ca.composer.Audit(evt.Context(), evt.Path())
	if err != nil {
		return workflow.Result[composer.Audit]{}, fmt.Errorf("failed to run composer audit after update: %w", err)
	}

	before, _ := workflow.Shared[composer.Audit](earlier, workflow.EventPreComposerUpdate)
	ca.logger.Info("security advisories",
		zap.Int("fixed", len(fixedAdvisories(before.Finding, after))),
		zap.Int("unresolved", len(after.Advisories)),
		zap.Int("abandoned", len(abandonedPackages(after))),
	)

	return workflow.Result[composer.Audit]{Finding: after}, nil
}

// abandonedPackages returns the audit's abandoned packages, minus drupal/*: unsupported_modules
// reports those from drupal.org's release data, and twice reads as two findings.
func abandonedPackages(audit composer.Audit) []composer.AbandonedPackage {
	abandoned := make([]composer.AbandonedPackage, 0, len(audit.Abandoned))
	for _, pkg := range audit.Abandoned {
		if strings.HasPrefix(pkg.PackageName, "drupal/") {
			continue
		}
		abandoned = append(abandoned, pkg)
	}
	return abandoned
}

// fixedAdvisories returns the advisories present before the update but not after.
func fixedAdvisories(before, after composer.Audit) []composer.Advisory {
	afterKeys := make(map[string]bool, len(after.Advisories))
	for _, afterAdvisory := range after.Advisories {
		afterKeys[advisoryKey(afterAdvisory)] = true
	}

	var fixed = make([]composer.Advisory, 0)
	for _, beforeAdvisory := range before.Advisories {
		if !afterKeys[advisoryKey(beforeAdvisory)] {
			fixed = append(fixed, beforeAdvisory)
		}
	}
	return fixed
}

// advisoryKey identifies an advisory without collapsing the CVE-less ones onto one key: a
// collision reports an advisory as fixed while it is still open. The last fallback quotes both
// halves rather than joining on a separator, because a title is free text.
func advisoryKey(a composer.Advisory) string {
	if a.CVE != "" {
		return "cve:" + a.CVE
	}
	if a.AdvisoryID != "" {
		return "id:" + a.AdvisoryID
	}
	return "pkg:" + strconv.Quote(a.PackageName) + strconv.Quote(a.Title)
}
