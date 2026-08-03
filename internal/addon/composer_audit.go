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

// ComposerAudit runs on every update: a routine update that closes a CVE should say so. Only a
// security run lets the audit dictate the update's scope, abort, and relabel the merge request.
type ComposerAudit struct {
	internal.BasicAddon
	logger   *zap.Logger
	composer Composer
	security bool
	current  time.Time

	beforeAudit composer.Audit
	afterAudit  composer.Audit
}

// NewComposerAudit creates a security auditor. security marks a `--security` run.
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
		// BelowNormal: audits the final code, so it must run after the addons that rewrite it.
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

// RenderTemplate renders nothing when there are no advisories at all, so a routine merge request
// carries no empty security section. Abandoned packages go to unsupported_modules instead.
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

// preComposerUpdateHandler audits the pre-update code. A security run narrows the update to the
// affected packages and aborts when there are none; a normal run only records the audit.
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

	// Deduplicated: several advisories often name one package, and this becomes composer's args.
	packagesToUpdate := make([]string, 0)
	seen := make(map[string]bool, len(ca.beforeAudit.Advisories))
	for _, advisory := range ca.beforeAudit.Advisories {
		if seen[advisory.PackageName] {
			continue
		}
		seen[advisory.PackageName] = true
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

// preMergeRequestCreateHandler publishes the abandoned packages for unsupported_modules and, on a
// security run, relabels the merge request. Runs at Normal, above the BelowNormal consumer.
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
