package addon

import (
	"fmt"
	"slices"
	"time"

	"github.com/drupdater/drupdater/pkg/composer"
	"github.com/gookit/event"

	"go.uber.org/zap"
)

type SecurityReport struct {
	FixedAdvisories       []composer.Advisory
	AfterUpdateAdvisories []composer.Advisory
	NumUnresolvedIssues   int
}

// ComposerAudit handles updating translations for Drupal sites
type ComposerAudit struct {
	BasicAddon
	logger   *zap.Logger
	composer composer.Runner
	current  time.Time

	beforeAudit composer.Audit
	afterAudit  composer.Audit
}

// NewComposerAudit creates a new translations updater instance
func NewComposerAudit(logger *zap.Logger, composer composer.Runner) *ComposerAudit {
	return &ComposerAudit{
		logger:   logger,
		composer: composer,
		current:  time.Now(),
	}
}

// SubscribedEvents returns the events this addon listens to
func (tu *ComposerAudit) SubscribedEvents() map[string]interface{} {
	return map[string]interface{}{
		"pre-composer-update": event.ListenerItem{
			Priority: event.Max,
			Listener: event.ListenerFunc(tu.preComposerUpdateHandler),
		},
		"post-code-update": event.ListenerItem{
			Priority: event.Normal,
			Listener: event.ListenerFunc(tu.postCodeUpdateHandler),
		},
		"pre-merge-request-create": event.ListenerItem{
			Priority: event.Normal,
			Listener: event.ListenerFunc(tu.preMergeRequestCreateHandler),
		},
	}
}

// RenderTemplate returns the rendered template for this addon
func (tu *ComposerAudit) RenderTemplate() (string, error) {
	return tu.Render("security_report.go.tmpl", SecurityReport{
		FixedAdvisories:       tu.GetFixedAdvisories(),
		AfterUpdateAdvisories: tu.afterAudit.Advisories,
		NumUnresolvedIssues:   len(tu.afterAudit.Advisories),
	})
}

func (tu *ComposerAudit) preComposerUpdateHandler(e event.Event) error {
	evt := e.(*PreComposerUpdateEvent)
	var err error

	tu.beforeAudit, err = tu.composer.Audit(evt.Context(), evt.Path())
	if err != nil {
		return err
	}

	packagesToUpdate := make([]string, 0)
	for _, advisory := range tu.beforeAudit.Advisories {
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
		tu.logger.Warn("no security advisories found, skipping security update")
		evt.Abort(true)
	}

	return nil
}

func (tu *ComposerAudit) postCodeUpdateHandler(e event.Event) error {
	evt := e.(*PostCodeUpdateEvent)

	var err error
	tu.afterAudit, err = tu.composer.Audit(evt.Context(), evt.Path())
	if err != nil {
		return err
	}

	return nil
}

func (tu *ComposerAudit) GetFixedAdvisories() []composer.Advisory {
	// Get advisories from before that are not present in after
	var fixed = make([]composer.Advisory, 0)
	for _, beforeAdvisory := range tu.beforeAudit.Advisories {
		found := false
		for _, afterAdvisory := range tu.afterAudit.Advisories {
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

func (tu *ComposerAudit) preMergeRequestCreateHandler(e event.Event) error {
	evt := e.(*PreMergeRequestCreateEvent)

	evt.Title = fmt.Sprintf("%s: Drupal Security Updates", tu.current.Format("2006-01-02"))

	return nil
}
