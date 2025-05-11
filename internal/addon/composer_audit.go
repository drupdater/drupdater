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
	NumUnresolvedIssues   int
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
func (ca *ComposerAudit) SubscribedEvents() map[string]interface{} {
	return map[string]interface{}{
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
		NumUnresolvedIssues:   len(ca.afterAudit.Advisories),
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
		ca.logger.Warn("no security advisories found, skipping security update")
		evt.Abort(true)
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

	return nil
}

// GetFixedAdvisories returns the list of security advisories that were fixed by the update.
func (ca *ComposerAudit) GetFixedAdvisories() []composer.Advisory {
	// Get advisories from before that are not present in after
	var fixed = make([]composer.Advisory, 0)
	for _, beforeAdvisory := range ca.beforeAudit.Advisories {
		found := false
		for _, afterAdvisory := range ca.afterAudit.Advisories {
			if beforeAdvisory.CVE == afterAdvisory.CVE {
				found = true
				break
			}
		}
		if !found {
			fixed = append(fixed, beforeAdvisory)
		}
	}
	return fixed
}

func (ca *ComposerAudit) preMergeRequestCreateHandler(e event.Event) error {
	evt := e.(*services.PreMergeRequestCreateEvent)

	evt.Title = fmt.Sprintf("%s: Drupal Security Updates", ca.current.Format("2006-01-02"))

	return nil
}
