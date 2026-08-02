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

// SecurityReport contains information about security advisories before and after updates.
type SecurityReport struct {
	FixedAdvisories       []composer.Advisory
	AfterUpdateAdvisories []composer.Advisory
}

// ComposerAudit handles security auditing for Drupal sites.
//
// It runs on every update, not only a security one, because `composer audit` answers two
// questions and only the first is specific to `--security`: which packages must be updated
// *now*, and what the security posture of the resulting code is. The second is worth having on
// a routine maintenance update too — a monthly update that happens to close a CVE should say
// so — and it is also where the abandoned-package list comes from.
//
// security is what separates the two. Only a security run lets the audit dictate the update's
// scope, abort when there is nothing to fix, and relabel the merge request; a normal run audits
// and reports, and otherwise keeps out of the way.
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

// SubscribedEvents returns the events this addon listens to.
func (ca *ComposerAudit) SubscribedEvents() map[string]any {
	return map[string]any{
		"pre-composer-update": event.ListenerItem{
			Priority: event.Max,
			Listener: event.ListenerFunc(ca.preComposerUpdateHandler),
		},
		// Below Normal: this reports the security posture of the final code, so it must run
		// after code_beautifier and deprecations_remover — both Normal/AboveNormal — rather than
		// possibly interleaving with them at an arbitrary point.
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
// A run with no advisories at all — neither closed nor open — renders nothing. On a security
// run that cannot happen, since the run aborts before it gets here; on a normal run it is the
// common case, and a "🛡️ Security Report" section saying only that there was nothing to
// report would appear in every routine merge request.
//
// The abandoned packages the same audit found are deliberately not rendered here. They are
// handed to unsupported_modules through pre-merge-request-create, which renders them together
// with the unsupported modules: to a reviewer both are the same finding.
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
// On a security run it then narrows the update to the affected packages and aborts when there
// are none. On a normal run it records the audit and returns: the update's scope is the user's
// to decide, and an update with nothing security-relevant to do still has plenty to do.
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

// GetAbandonedPackages returns the packages composer reported as abandoned, taken from the
// audit run after the update so the list describes the code the merge request actually
// contains — the same reason the remaining advisories come from that run. A package that was
// abandoned before and is no longer abandoned after is simply absent, which is the right
// answer: unlike an advisory, there is nothing for the update to take credit for.
//
// drupal/* packages are left out. unsupported_modules already reports those, from drupal.org's
// own release data, which knows whether a module is supported regardless of what the Packagist
// mirror says. Listing them in both places would show one problem twice, in two sections of the
// same merge request, as though they were separate findings.
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

// GetFixedAdvisories returns the list of security advisories that were fixed by the update:
// those present before but not after. Advisories are identified by CVE, falling back to the
// advisory ID (and finally package+title) so advisories without a CVE — which would all share
// the empty string — don't collide and get miscounted.
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

// advisoryKey returns a stable identity for an advisory that does not collapse distinct
// advisories which happen to lack a CVE.
//
// The last fallback quotes both halves rather than joining them on a separator. A plain
// separator is only unambiguous while neither field contains it, and an advisory title is free
// text — "Access bypass | SA-CONTRIB" is an ordinary thing for one to say. Two different
// advisories sharing a key here means one of them is reported as fixed while it is still open,
// in a security update.
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
// It subscribes at Normal while unsupported_modules subscribes at BelowNormal, so the list is
// on the event before the addon that renders it reads it. Priority is load-bearing here.
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
