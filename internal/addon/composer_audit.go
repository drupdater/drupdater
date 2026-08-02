package addon

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/drupdater/drupdater/internal"
	"github.com/drupdater/drupdater/internal/services"
	"github.com/drupdater/drupdater/pkg/composer"
	"github.com/gookit/event"

	"go.uber.org/zap"
)

// SecurityReport holds the advisories from before and after the update.
type SecurityReport struct {
	FixedAdvisories       []composer.Advisory
	AfterUpdateAdvisories []composer.Advisory
}

// ComposerAudit handles security auditing for Drupal sites.
//
// It runs on every update because `composer audit` answers two questions and only one is
// specific to --security: which packages must be updated *now*, and what the resulting code's
// security posture is. A monthly update that happens to close a CVE should say so, and the
// abandoned-package list comes from the same audit.
//
// The security flag separates the two: only a security run lets the audit dictate the update's
// scope, abort when there is nothing to fix, and relabel the merge request.
type ComposerAudit struct {
	internal.BasicAddon
	logger   *zap.Logger
	composer Composer
	security bool
	current  time.Time

	beforeAudit composer.Audit
	afterAudit  composer.Audit
}

// NewComposerAudit creates a new security auditor instance. security marks a `--security` run,
// in which the audit drives the update rather than only describing it.
func NewComposerAudit(logger *zap.Logger, composer Composer, security bool) *ComposerAudit {
	return &ComposerAudit{
		logger:   logger,
		composer: composer,
		security: security,
		current:  time.Now(),
	}
}

func (ca *ComposerAudit) SubscribedEvents() map[string]any {
	return map[string]any{
		"pre-composer-update": event.ListenerItem{
			Priority: event.Max,
			Listener: event.ListenerFunc(ca.preComposerUpdateHandler),
		},
		// Below Normal: this reports the final code's security posture, so it must run after
		// code_beautifier and deprecations_remover rather than interleaving with them.
		"post-code-update": event.ListenerItem{
			Priority: event.BelowNormal,
			Listener: event.ListenerFunc(ca.postCodeUpdateHandler),
		},
		"pre-merge-request-create": event.ListenerItem{
			Priority: event.Normal,
			Listener: event.ListenerFunc(ca.preMergeRequestCreateHandler),
		},
	}
}

// RenderTemplate returns the rendered template for this addon.
//
// A run with no advisories at all renders nothing — the common case on a normal run, where a
// "🛡️ Security Report" section saying nothing would appear in every routine merge request.
//
// The abandoned packages go to unsupported_modules via pre-merge-request-create instead, which
// renders both as one list: to a reviewer they are the same finding.
func (ca *ComposerAudit) RenderTemplate() (string, error) {
	fixed := ca.GetFixedAdvisories()
	if len(fixed) == 0 && len(ca.afterAudit.Advisories) == 0 {
		return "", nil
	}

	return ca.Render("security_report.go.tmpl", SecurityReport{
		FixedAdvisories:       fixed,
		AfterUpdateAdvisories: ca.afterAudit.Advisories,
	})
}

// preComposerUpdateHandler audits the code as it stands before the update.
//
// On a security run it narrows the update to the affected packages and aborts when there are
// none. On a normal run it only records the audit: the scope is the user's to decide.
func (ca *ComposerAudit) preComposerUpdateHandler(e event.Event) error {
	evt := e.(*services.PreComposerUpdateEvent)
	var err error

	ca.beforeAudit, err = ca.composer.Audit(evt.Context(), evt.Path())
	if err != nil {
		return fmt.Errorf("failed to run composer audit: %w", err)
	}

	if !ca.security {
		return nil
	}

	packagesToUpdate := make([]string, 0)
	for _, advisory := range ca.beforeAudit.Advisories {
		if slices.Contains(packagesToUpdate, advisory.PackageName) {
			continue
		}
		packagesToUpdate = append(packagesToUpdate, advisory.PackageName)
	}

	if slices.Contains(packagesToUpdate, "drupal/core") {
		packagesToUpdate = append(packagesToUpdate, "drupal/core-recommended")
		packagesToUpdate = append(packagesToUpdate, "drupal/core-composer-scaffold")
	}

	evt.PackagesToUpdate = packagesToUpdate
	evt.MinimalChanges = true

	if len(packagesToUpdate) == 0 {
		return services.AbortError{Msg: "No security advisories found"}
	}

	return nil
}

func (ca *ComposerAudit) postCodeUpdateHandler(e event.Event) error {
	evt := e.(*services.PostCodeUpdateEvent)

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

// GetAbandonedPackages returns the abandoned packages from the post-update audit, so the list
// describes the code the merge request actually contains. Unlike an advisory, one that is no
// longer abandoned is simply absent — there is nothing for the update to take credit for.
//
// drupal/* is left out: unsupported_modules already reports those from drupal.org's own release
// data, and listing them twice would read as two separate findings.
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

// GetFixedAdvisories returns the advisories present before the update but not after. Identified
// by CVE, falling back to advisory ID and then package+title, so the CVE-less ones don't all
// collide on the empty string.
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

// advisoryKey returns a stable identity that does not collapse distinct advisories lacking a CVE.
//
// The last fallback quotes both halves instead of joining on a separator: a title is free text
// and may contain one. Two advisories colliding here means one is reported as fixed while it is
// still open, in a security update.
func advisoryKey(a composer.Advisory) string {
	if a.CVE != "" {
		return "cve:" + a.CVE
	}
	if a.AdvisoryID != "" {
		return "id:" + a.AdvisoryID
	}
	return "pkg:" + strconv.Quote(a.PackageName) + strconv.Quote(a.Title)
}

// preMergeRequestCreateHandler publishes the abandoned packages for unsupported_modules to
// render, and — on a security run only — relabels the merge request.
//
// Priority is load-bearing: this runs at Normal and unsupported_modules at BelowNormal, so the
// list is on the event before the addon that renders it reads it.
func (ca *ComposerAudit) preMergeRequestCreateHandler(e event.Event) error {
	evt := e.(*services.PreMergeRequestCreateEvent)

	for _, pkg := range ca.GetAbandonedPackages() {
		evt.AbandonedPackages = append(evt.AbandonedPackages, services.AbandonedPackage{
			Name:        pkg.PackageName,
			Replacement: pkg.Replacement,
		})
	}

	if ca.security {
		evt.Title = fmt.Sprintf("%s: Drupal Security Updates", ca.current.Format("2006-01-02"))
	}

	return nil
}
