package addon

import (
	"fmt"
	"slices"
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
type ComposerAudit struct {
	internal.BasicAddon
	logger   *zap.Logger
	composer Composer
	current  time.Time

	beforeAudit composer.Audit
	afterAudit  composer.Audit
}

// NewComposerAudit creates a new security auditor instance.
func NewComposerAudit(logger *zap.Logger, composer Composer) *ComposerAudit {
	return &ComposerAudit{
		logger:   logger,
		composer: composer,
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
		"post-code-update": event.ListenerItem{
			Priority: event.Normal,
			Listener: event.ListenerFunc(ca.postCodeUpdateHandler),
		},
		"pre-merge-request-create": event.ListenerItem{
			Priority: event.Normal,
			Listener: event.ListenerFunc(ca.preMergeRequestCreateHandler),
		},
	}
}

// RenderTemplate returns the rendered template for this addon.
func (ca *ComposerAudit) RenderTemplate() (string, error) {
	return ca.Render("security_report.go.tmpl", SecurityReport{
		FixedAdvisories:       ca.GetFixedAdvisories(),
		AfterUpdateAdvisories: ca.afterAudit.Advisories,
	})
}

func (ca *ComposerAudit) preComposerUpdateHandler(e event.Event) error {
	evt := e.(*services.PreComposerUpdateEvent)
	var err error

	ca.beforeAudit, err = ca.composer.Audit(evt.Context(), evt.Path())
	if err != nil {
		return fmt.Errorf("failed to run composer audit: %w", err)
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
	)

	return nil
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
func advisoryKey(a composer.Advisory) string {
	if a.CVE != "" {
		return "cve:" + a.CVE
	}
	if a.AdvisoryID != "" {
		return "id:" + a.AdvisoryID
	}
	return "pkg:" + a.PackageName + "|" + a.Title
}

func (ca *ComposerAudit) preMergeRequestCreateHandler(e event.Event) error {
	evt := e.(*services.PreMergeRequestCreateEvent)

	evt.Title = fmt.Sprintf("%s: Drupal Security Updates", ca.current.Format("2006-01-02"))

	return nil
}
