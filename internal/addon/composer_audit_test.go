package addon

import (
	"context"
	"testing"
	"time"

	"github.com/drupdater/drupdater/internal/golden"
	"github.com/drupdater/drupdater/internal/services"
	"github.com/drupdater/drupdater/pkg/composer"
	"github.com/gookit/event"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// TestNewComposerAudit tests the constructor
func TestNewComposerAudit(t *testing.T) {
	logger := zap.NewNop()
	mockComposer := NewMockComposer(t)

	before := time.Now()
	audit := NewComposerAudit(logger, mockComposer, true)
	after := time.Now()

	assert.NotNil(t, audit)
	assert.Equal(t, logger, audit.logger)
	assert.Equal(t, mockComposer, audit.composer)
	assert.True(t, audit.current.After(before) || audit.current.Equal(before))
	assert.True(t, audit.current.Before(after) || audit.current.Equal(after))
}

// TestComposerAudit_SubscribedEvents tests the event subscription
func TestComposerAudit_SubscribedEvents(t *testing.T) {
	audit := &ComposerAudit{}

	events := audit.SubscribedEvents()

	assert.Contains(t, events, "pre-composer-update")
	assert.Contains(t, events, "post-code-update")
	assert.Contains(t, events, "pre-merge-request-create")

	preComposerEvent := events["pre-composer-update"].(event.ListenerItem)
	assert.Equal(t, event.Max, preComposerEvent.Priority)

	postCodeEvent := events["post-code-update"].(event.ListenerItem)
	assert.Equal(t, event.BelowNormal, postCodeEvent.Priority)

	preMergeEvent := events["pre-merge-request-create"].(event.ListenerItem)
	assert.Equal(t, event.Normal, preMergeEvent.Priority)
}

// TestComposerAudit_PreComposerUpdateHandler_WithAdvisories tests handling security advisories
func TestComposerAudit_PreComposerUpdateHandler_WithAdvisories(t *testing.T) {
	logger := zap.NewNop()
	mockComposer := NewMockComposer(t)
	audit := NewComposerAudit(logger, mockComposer, true)
	worktree := NewMockWorktree(t)

	ctx := context.Background()
	path := "/test/path"

	mockAudit := composer.Audit{
		Advisories: []composer.Advisory{
			{
				PackageName: "drupal/core",
				CVE:         "CVE-2023-1234",
				Title:       "Security issue in Drupal core",
			},
			{
				PackageName: "other/package",
				CVE:         "CVE-2023-5678",
				Title:       "Security issue in another package",
			},
		},
	}

	mockEvent := services.NewPreComposerUpdateEvent(ctx, path, worktree, []string{}, []string{}, true)

	mockComposer.EXPECT().Audit(ctx, path).Return(mockAudit, nil)

	err := audit.preComposerUpdateHandler(mockEvent)

	require.NoError(t, err)
	assert.Equal(t, mockAudit, audit.beforeAudit)
	assert.Equal(t, []string{"drupal/core", "other/package", "drupal/core-recommended", "drupal/core-composer-scaffold"}, mockEvent.PackagesToUpdate)
	assert.True(t, mockEvent.MinimalChanges)
	assert.False(t, mockEvent.IsAborted())
}

// TestComposerAudit_PreComposerUpdateHandler_NoAdvisories tests when no security advisories are found
func TestComposerAudit_PreComposerUpdateHandler_NoAdvisories(t *testing.T) {
	logger := zap.NewNop()
	mockComposer := NewMockComposer(t)
	audit := NewComposerAudit(logger, mockComposer, true)
	worktree := NewMockWorktree(t)

	ctx := context.Background()
	path := "/test/path"

	mockAudit := composer.Audit{
		Advisories: []composer.Advisory{},
	}

	mockEvent := services.NewPreComposerUpdateEvent(ctx, path, worktree, []string{}, []string{}, true)

	mockComposer.EXPECT().Audit(ctx, path).Return(mockAudit, nil)

	err := audit.preComposerUpdateHandler(mockEvent)

	require.ErrorAs(t, err, &services.AbortError{})
	assert.Equal(t, "No security advisories found", err.Error())
	assert.Equal(t, mockAudit, audit.beforeAudit)
	assert.Empty(t, mockEvent.PackagesToUpdate)
}

// TestComposerAudit_PostCodeUpdateHandler tests the post-code-update handler
func TestComposerAudit_PostCodeUpdateHandler(t *testing.T) {
	logger := zap.NewNop()
	mockComposer := NewMockComposer(t)
	worktree := NewMockWorktree(t)
	audit := NewComposerAudit(logger, mockComposer, true)

	ctx := context.Background()
	path := "/test/path"

	mockAudit := composer.Audit{
		Advisories: []composer.Advisory{
			{
				PackageName: "other/package",
				CVE:         "CVE-2023-5678",
				Title:       "Unresolved security issue",
			},
		},
	}

	mockEvent := services.NewPostCodeUpdateEvent(ctx, path, worktree)

	mockComposer.EXPECT().Audit(ctx, path).Return(mockAudit, nil)

	err := audit.postCodeUpdateHandler(mockEvent)

	require.NoError(t, err)
	assert.Equal(t, mockAudit, audit.afterAudit)
}

func TestAdvisoryKey(t *testing.T) {
	// CVE takes precedence; without it the advisory ID is used; without either, package+title.
	assert.Equal(t, "cve:CVE-1", advisoryKey(composer.Advisory{CVE: "CVE-1", AdvisoryID: "A1"}))
	assert.Equal(t, "id:A1", advisoryKey(composer.Advisory{AdvisoryID: "A1", PackageName: "drupal/foo"}))
	assert.Equal(t, `pkg:"drupal/foo""Title"`, advisoryKey(composer.Advisory{PackageName: "drupal/foo", Title: "Title"}))
}

// A title is free text, so joining package and title on a separator lets two advisories share
// a key — and the second is then reported as fixed while it is still open.
func TestAdvisoryKey_SeparatorInTitle(t *testing.T) {
	split := composer.Advisory{PackageName: "drupal/foo|Access", Title: "bypass"}
	other := composer.Advisory{PackageName: "drupal/foo", Title: "Access|bypass"}

	assert.NotEqual(t, advisoryKey(split), advisoryKey(other))
}

func TestComposerAudit_GetFixedAdvisories_NoCVEAdvisoriesDoNotCollide(t *testing.T) {
	// Two advisories without a CVE must be treated as distinct: one fixed, one remaining.
	audit := &ComposerAudit{logger: zap.NewNop()}
	audit.beforeAudit = composer.Audit{Advisories: []composer.Advisory{
		{AdvisoryID: "A1", PackageName: "drupal/foo", Title: "foo"},
		{AdvisoryID: "A2", PackageName: "drupal/bar", Title: "bar"},
	}}
	audit.afterAudit = composer.Audit{Advisories: []composer.Advisory{
		{AdvisoryID: "A2", PackageName: "drupal/bar", Title: "bar"},
	}}

	fixed := audit.GetFixedAdvisories()
	require.Len(t, fixed, 1)
	assert.Equal(t, "A1", fixed[0].AdvisoryID)
}

// TestComposerAudit_GetFixedAdvisories tests the method that compares before/after advisories
func TestComposerAudit_GetFixedAdvisories(t *testing.T) {
	audit := &ComposerAudit{}

	audit.beforeAudit = composer.Audit{
		Advisories: []composer.Advisory{
			{
				PackageName: "drupal/core",
				CVE:         "CVE-2023-1234",
				Title:       "Fixed issue",
			},
			{
				PackageName: "other/package",
				CVE:         "CVE-2023-5678",
				Title:       "Unresolved issue",
			},
		},
	}

	audit.afterAudit = composer.Audit{
		Advisories: []composer.Advisory{
			{
				PackageName: "other/package",
				CVE:         "CVE-2023-5678",
				Title:       "Still exists after update",
			},
		},
	}

	fixed := audit.GetFixedAdvisories()

	assert.Len(t, fixed, 1)
	assert.Equal(t, "CVE-2023-1234", fixed[0].CVE)
	assert.Equal(t, "drupal/core", fixed[0].PackageName)
}

// TestComposerAudit_PreMergeRequestCreateHandler tests the merge request title generation
func TestComposerAudit_PreMergeRequestCreateHandler(t *testing.T) {
	fixedDate := time.Date(2023, 5, 15, 12, 0, 0, 0, time.UTC)
	audit := &ComposerAudit{
		current:  fixedDate,
		security: true,
	}

	mockEvent := &services.PreMergeRequestCreateEvent{}
	mockEvent.SetName("pre-merge-request-create")

	err := audit.preMergeRequestCreateHandler(mockEvent)

	require.NoError(t, err)
	assert.Equal(t, "2023-05-15: Drupal Security Updates", mockEvent.Title)
}

// TestComposerAudit_RenderTemplate tests template rendering
func TestComposerAudit_RenderTemplate(t *testing.T) {
	logger := zap.NewNop()
	mockComposer := NewMockComposer(t)
	audit := NewComposerAudit(logger, mockComposer, true)

	audit.beforeAudit = composer.Audit{
		Advisories: []composer.Advisory{
			{
				PackageName: "drupal/core",
				CVE:         "CVE-2023-1234",
				Title:       "Fixed issue",
			},
			{
				PackageName: "other/package",
				CVE:         "CVE-2023-5678",
				Title:       "Unresolved issue",
			},
		},
	}

	audit.afterAudit = composer.Audit{
		Advisories: []composer.Advisory{
			{
				PackageName: "other/package",
				CVE:         "CVE-2023-5678",
				Title:       "Still unresolved",
			},
		},
	}

	result, err := audit.RenderTemplate()
	require.NoError(t, err)
	golden.Assert(t, "testdata/composer_audit.md", result)
}

// TestComposerAudit_RenderTemplate_EscapesPipes ensures a "|" in an advisory
// title is escaped so it can't break out of the markdown table cell.
func TestComposerAudit_RenderTemplate_EscapesPipes(t *testing.T) {
	audit := NewComposerAudit(zap.NewNop(), NewMockComposer(t), true)
	audit.afterAudit = composer.Audit{
		Advisories: []composer.Advisory{
			{PackageName: "drupal/foo", CVE: "CVE-1", Title: "XSS via a|b\nsecond line"},
		},
	}

	result, err := audit.RenderTemplate()
	require.NoError(t, err)
	assert.Contains(t, result, "XSS via a\\|b second line")
	assert.NotContains(t, result, "a|b")
}

// drupal/* is left to unsupported_modules, so one module is not reported twice.
func TestComposerAudit_GetAbandonedPackages_FiltersDrupalPackages(t *testing.T) {
	audit := NewComposerAudit(zap.NewNop(), NewMockComposer(t), true)
	audit.afterAudit = composer.Audit{
		Abandoned: []composer.AbandonedPackage{
			{PackageName: "drupal/token", Replacement: "drupal/core"},
			{PackageName: "drupalfinder/drupal-finder", Replacement: "webflo/drupal-finder"},
			{PackageName: "swiftmailer/swiftmailer", Replacement: "symfony/mailer"},
		},
	}

	// drupalfinder/drupal-finder stays: only the drupal/ vendor is drupal.org's, and a prefix
	// match on "drupal" alone would swallow unrelated vendors.
	assert.Equal(t, []composer.AbandonedPackage{
		{PackageName: "drupalfinder/drupal-finder", Replacement: "webflo/drupal-finder"},
		{PackageName: "swiftmailer/swiftmailer", Replacement: "symfony/mailer"},
	}, audit.GetAbandonedPackages())
}

// TestComposerAudit_GetAbandonedPackages_UsesTheAuditAfterTheUpdate checks the list describes
// the code the merge request contains, not the code it started from.
func TestComposerAudit_GetAbandonedPackages_UsesTheAuditAfterTheUpdate(t *testing.T) {
	audit := NewComposerAudit(zap.NewNop(), NewMockComposer(t), true)
	audit.beforeAudit = composer.Audit{
		Abandoned: []composer.AbandonedPackage{{PackageName: "gone/away"}},
	}
	audit.afterAudit = composer.Audit{
		Abandoned: []composer.AbandonedPackage{{PackageName: "still/here"}},
	}

	assert.Equal(t, []composer.AbandonedPackage{{PackageName: "still/here"}}, audit.GetAbandonedPackages())
}

// A normal run audits without taking over: narrowing or aborting here would turn every normal
// run into a security run.
func TestComposerAudit_PreComposerUpdateHandler_NormalMode(t *testing.T) {
	mockComposer := NewMockComposer(t)
	audit := NewComposerAudit(zap.NewNop(), mockComposer, false)
	worktree := NewMockWorktree(t)

	ctx := context.Background()
	path := "/test/path"
	mockAudit := composer.Audit{
		Advisories: []composer.Advisory{{PackageName: "drupal/core", CVE: "CVE-2023-1234"}},
	}
	mockComposer.EXPECT().Audit(ctx, path).Return(mockAudit, nil)

	mockEvent := services.NewPreComposerUpdateEvent(ctx, path, worktree, []string{}, []string{}, false)

	require.NoError(t, audit.preComposerUpdateHandler(mockEvent))
	assert.Equal(t, mockAudit, audit.beforeAudit, "the audit still runs: it is what the security report is built from")
	assert.Empty(t, mockEvent.PackagesToUpdate)
	assert.False(t, mockEvent.MinimalChanges)
}

// TestComposerAudit_PreComposerUpdateHandler_NormalModeNoAdvisories checks the abort path is
// security-only. On a normal run "no advisories" is the healthy case, not a reason to stop.
func TestComposerAudit_PreComposerUpdateHandler_NormalModeNoAdvisories(t *testing.T) {
	mockComposer := NewMockComposer(t)
	audit := NewComposerAudit(zap.NewNop(), mockComposer, false)
	worktree := NewMockWorktree(t)

	ctx := context.Background()
	path := "/test/path"
	mockComposer.EXPECT().Audit(ctx, path).Return(composer.Audit{}, nil)

	mockEvent := services.NewPreComposerUpdateEvent(ctx, path, worktree, []string{}, []string{}, false)

	require.NoError(t, audit.preComposerUpdateHandler(mockEvent))
}

// TestComposerAudit_PreMergeRequestCreateHandler_NormalMode checks a normal run keeps the
// maintenance title it was given, and still publishes its abandoned packages.
func TestComposerAudit_PreMergeRequestCreateHandler_NormalMode(t *testing.T) {
	audit := &ComposerAudit{current: time.Date(2023, 5, 15, 12, 0, 0, 0, time.UTC)}
	audit.afterAudit = composer.Audit{
		Abandoned: []composer.AbandonedPackage{{PackageName: "swiftmailer/swiftmailer", Replacement: "symfony/mailer"}},
	}

	mockEvent := &services.PreMergeRequestCreateEvent{Title: "July 2026: Drupal Maintenance Updates"}
	mockEvent.SetName("pre-merge-request-create")

	require.NoError(t, audit.preMergeRequestCreateHandler(mockEvent))
	assert.Equal(t, "July 2026: Drupal Maintenance Updates", mockEvent.Title)
	assert.Equal(t, []services.AbandonedPackage{
		{Name: "swiftmailer/swiftmailer", Replacement: "symfony/mailer"},
	}, mockEvent.AbandonedPackages)
}

// TestComposerAudit_PreMergeRequestCreateHandler_PublishesAbandonedPackages checks the handoff
// to unsupported_modules, which renders these together with the unsupported modules.
func TestComposerAudit_PreMergeRequestCreateHandler_PublishesAbandonedPackages(t *testing.T) {
	audit := &ComposerAudit{security: true, current: time.Now()}
	audit.afterAudit = composer.Audit{
		Abandoned: []composer.AbandonedPackage{
			{PackageName: "drupal/token"},
			{PackageName: "patchwork/jsqueeze"},
		},
	}

	mockEvent := &services.PreMergeRequestCreateEvent{}
	mockEvent.SetName("pre-merge-request-create")

	require.NoError(t, audit.preMergeRequestCreateHandler(mockEvent))
	// drupal/* is filtered out before the handoff, so the merged list cannot show one
	// end-of-life module as two rows.
	assert.Equal(t, []services.AbandonedPackage{{Name: "patchwork/jsqueeze"}}, mockEvent.AbandonedPackages)
}

// No advisories at all contributes no section, or every routine merge request carries an empty
// security report.
func TestComposerAudit_RenderTemplate_NothingToReport(t *testing.T) {
	audit := NewComposerAudit(zap.NewNop(), NewMockComposer(t), false)

	result, err := audit.RenderTemplate()
	require.NoError(t, err)
	assert.Empty(t, result)
}

// Abandoned packages go to unsupported_modules; rendering them here too shows one finding twice.
func TestComposerAudit_RenderTemplate_OmitsAbandonedPackages(t *testing.T) {
	audit := NewComposerAudit(zap.NewNop(), NewMockComposer(t), true)
	audit.afterAudit = composer.Audit{
		Advisories: []composer.Advisory{{PackageName: "drupal/core", CVE: "CVE-1", Title: "Open"}},
		Abandoned:  []composer.AbandonedPackage{{PackageName: "patchwork/jsqueeze"}},
	}

	result, err := audit.RenderTemplate()
	require.NoError(t, err)
	assert.NotContains(t, result, "patchwork/jsqueeze")
}

// The other half of the guard: a run that closed every advisory still has something to say.
func TestComposerAudit_RenderTemplate_FixedOnly(t *testing.T) {
	audit := NewComposerAudit(zap.NewNop(), NewMockComposer(t), true)
	audit.beforeAudit = composer.Audit{
		Advisories: []composer.Advisory{{PackageName: "drupal/core", CVE: "CVE-1", Title: "Closed"}},
	}

	result, err := audit.RenderTemplate()
	require.NoError(t, err)
	assert.Contains(t, result, "CVE-1")
	assert.Contains(t, result, "All security issues have been resolved.")
}

// A swallowed audit failure would let a security run continue with an empty advisory list,
// updating nothing and reporting that everything is fine.
func TestComposerAudit_AuditFailures(t *testing.T) {
	path := "/test/path"

	t.Run("before the update", func(t *testing.T) {
		mockComposer := NewMockComposer(t)
		audit := NewComposerAudit(zap.NewNop(), mockComposer, true)
		mockComposer.EXPECT().Audit(anyCtx, path).Return(composer.Audit{}, assert.AnError)

		evt := services.NewPreComposerUpdateEvent(context.Background(), path, NewMockWorktree(t), []string{}, []string{}, true)
		err := audit.preComposerUpdateHandler(evt)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to run composer audit")
		// Not an abort: aborting reads as "nothing to do here" and exits 0.
		assert.False(t, evt.IsAborted())
		assert.Empty(t, evt.PackagesToUpdate)
	})

	t.Run("after the update", func(t *testing.T) {
		mockComposer := NewMockComposer(t)
		audit := NewComposerAudit(zap.NewNop(), mockComposer, true)
		mockComposer.EXPECT().Audit(anyCtx, path).Return(composer.Audit{}, assert.AnError)

		evt := services.NewPostCodeUpdateEvent(context.Background(), path, NewMockWorktree(t))
		err := audit.postCodeUpdateHandler(evt)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to run composer audit after update")
	})
}

// The list becomes composer update's arguments, which must not name a package twice.
func TestComposerAudit_PreComposerUpdateHandler_DeduplicatesPackages(t *testing.T) {
	mockComposer := NewMockComposer(t)
	audit := NewComposerAudit(zap.NewNop(), mockComposer, true)
	path := "/test/path"

	mockComposer.EXPECT().Audit(anyCtx, path).Return(composer.Audit{
		Advisories: []composer.Advisory{
			{PackageName: "drupal/webform", CVE: "CVE-1", Title: "first"},
			{PackageName: "drupal/webform", CVE: "CVE-2", Title: "second"},
			{PackageName: "other/package", CVE: "CVE-3", Title: "third"},
			{PackageName: "drupal/webform", CVE: "CVE-4", Title: "fourth"},
		},
	}, nil)

	evt := services.NewPreComposerUpdateEvent(context.Background(), path, NewMockWorktree(t), []string{}, []string{}, true)
	require.NoError(t, audit.preComposerUpdateHandler(evt))

	// First-seen order, not sorted: the order is the order composer receives the arguments in.
	assert.Equal(t, []string{"drupal/webform", "other/package"}, evt.PackagesToUpdate)
}
