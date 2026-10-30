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

	beforeAudit composer.Audit
	afterAudit  composer.Audit
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
		PreComposerUpdate: workflow.Propose(workflow.Prepare, ca.preComposerUpdateHandler),
		PostCodeUpdate:    workflow.On(workflow.Inspect, ca.postCodeUpdateHandler),
	}
}

// preComposerUpdateHandler audits the pre-update code. A security run narrows the update to the
// affected packages and aborts when there are none; a normal run only records the audit.
func (ca *ComposerAudit) preComposerUpdateHandler(evt *workflow.PreComposerUpdateEvent) (workflow.UpdatePlan, error) {
	var err error

	ca.beforeAudit, err = ca.composer.Audit(evt.Context(), evt.Path())
	if err != nil {
		return workflow.UpdatePlan{}, fmt.Errorf("failed to run composer audit: %w", err)
	}

	if !ca.security {
		return workflow.UpdatePlan{}, nil
	}

	// Several advisories often name one package; merging the plan deduplicates them.
	packagesToUpdate := make([]string, 0, len(ca.beforeAudit.Advisories))
	for _, advisory := range ca.beforeAudit.Advisories {
		packagesToUpdate = append(packagesToUpdate, advisory.PackageName)
	}

	if slices.Contains(packagesToUpdate, "drupal/core") {
		packagesToUpdate = append(packagesToUpdate, "drupal/core-recommended")
		packagesToUpdate = append(packagesToUpdate, "drupal/core-composer-scaffold")
	}

	if len(packagesToUpdate) == 0 {
		return workflow.UpdatePlan{}, workflow.AbortError{Msg: "No security advisories found"}
	}

	return workflow.UpdatePlan{Only: packagesToUpdate, MinimalChanges: true}, nil
}

func (ca *ComposerAudit) postCodeUpdateHandler(evt *workflow.PostCodeUpdateEvent) error {

	var err error
	ca.afterAudit, err = ca.composer.Audit(evt.Context(), evt.Path())
	if err != nil {
		return fmt.Errorf("failed to run composer audit after update: %w", err)
	}

	ca.logger.Info("security advisories",
		zap.Int("fixed", len(ca.GetFixedAdvisories())),
		zap.Int("unresolved", len(ca.afterAudit.Advisories)),
		zap.Int("abandoned", len(ca.GetAbandonedPackages())),
	)

	return nil
}

// GetAbandonedPackages returns the post-update audit's abandoned packages, minus drupal/*:
// unsupported_modules reports those from drupal.org's release data, and twice reads as two findings.
func (ca *ComposerAudit) GetAbandonedPackages() []composer.AbandonedPackage {
	abandoned := make([]composer.AbandonedPackage, 0, len(ca.afterAudit.Abandoned))
	for _, pkg := range ca.afterAudit.Abandoned {
		if strings.HasPrefix(pkg.PackageName, "drupal/") {
			continue
		}
		abandoned = append(abandoned, pkg)
	}
	return abandoned
}

// GetFixedAdvisories returns the advisories present before the update but not after.
func (ca *ComposerAudit) GetFixedAdvisories() []composer.Advisory {
	afterKeys := make(map[string]bool, len(ca.afterAudit.Advisories))
	for _, afterAdvisory := range ca.afterAudit.Advisories {
		afterKeys[advisoryKey(afterAdvisory)] = true
	}

	var fixed = make([]composer.Advisory, 0)
	for _, beforeAdvisory := range ca.beforeAudit.Advisories {
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
