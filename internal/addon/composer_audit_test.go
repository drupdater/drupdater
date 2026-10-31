package addon

import (
	"context"
	"testing"

	"github.com/drupdater/drupdater/internal/composer"
	"github.com/drupdater/drupdater/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// TestNewComposerAudit tests the constructor
func TestNewComposerAudit(t *testing.T) {
	logger := zap.NewNop()
	mockComposer := NewMockAuditComposer(t)

	audit := NewComposerAudit(logger, mockComposer, true)

	assert.NotNil(t, audit)
	assert.Equal(t, logger, audit.logger)
	assert.Equal(t, mockComposer, audit.composer)
	assert.True(t, audit.security)
}

// TestComposerAudit_PreComposerUpdateHandler_WithAdvisories tests handling security advisories
func TestComposerAudit_PreComposerUpdateHandler_WithAdvisories(t *testing.T) {
	logger := zap.NewNop()
	mockComposer := NewMockAuditComposer(t)
	audit := NewComposerAudit(logger, mockComposer, true)
	committer := &fakeCommitter{}

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

	mockEvent := workflow.NewPreComposerUpdateEvent(ctx, path, committer, workflow.UpdatePlan{})

	mockComposer.EXPECT().Audit(ctx, path).Return(mockAudit, nil)

	proposal, err := audit.preComposerUpdateHandler(mockEvent)

	require.NoError(t, err)
	assert.Equal(t, mockAudit, proposal.Finding)
	assert.Equal(t, workflow.UpdatePlan{
		Only:           []string{"drupal/core", "other/package", "drupal/core-recommended", "drupal/core-composer-scaffold"},
		MinimalChanges: true,
	}, proposal.Plan)
}

// TestComposerAudit_PreComposerUpdateHandler_NoAdvisories tests when no security advisories are found
func TestComposerAudit_PreComposerUpdateHandler_NoAdvisories(t *testing.T) {
	logger := zap.NewNop()
	mockComposer := NewMockAuditComposer(t)
	audit := NewComposerAudit(logger, mockComposer, true)
	committer := &fakeCommitter{}

	ctx := context.Background()
	path := "/test/path"

	mockAudit := composer.Audit{
		Advisories: []composer.Advisory{},
	}

	mockEvent := workflow.NewPreComposerUpdateEvent(ctx, path, committer, workflow.UpdatePlan{})

	mockComposer.EXPECT().Audit(ctx, path).Return(mockAudit, nil)

	proposal, err := audit.preComposerUpdateHandler(mockEvent)

	require.ErrorAs(t, err, &workflow.AbortError{})
	assert.Equal(t, "No security advisories found", err.Error())
	assert.Zero(t, proposal, "an abort records nothing")
}

// TestComposerAudit_PostCodeUpdateHandler tests the post-code-update handler
func TestComposerAudit_PostCodeUpdateHandler(t *testing.T) {
	logger := zap.NewNop()
	mockComposer := NewMockAuditComposer(t)
	committer := &fakeCommitter{}
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

	mockEvent := workflow.NewPostCodeUpdateEvent(ctx, path, committer)

	mockComposer.EXPECT().Audit(ctx, path).Return(mockAudit, nil)

	result, err := audit.postCodeUpdateHandler(mockEvent, &workflow.Findings{})

	require.NoError(t, err)
	assert.Equal(t, mockAudit, result.Finding)
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

func TestFixedAdvisories_NoCVEAdvisoriesDoNotCollide(t *testing.T) {
	// Two advisories without a CVE must be treated as distinct: one fixed, one remaining.
	before := composer.Audit{Advisories: []composer.Advisory{
		{AdvisoryID: "A1", PackageName: "drupal/foo", Title: "foo"},
		{AdvisoryID: "A2", PackageName: "drupal/bar", Title: "bar"},
	}}
	after := composer.Audit{Advisories: []composer.Advisory{
		{AdvisoryID: "A2", PackageName: "drupal/bar", Title: "bar"},
	}}

	fixed := fixedAdvisories(before, after)
	require.Len(t, fixed, 1)
	assert.Equal(t, "A1", fixed[0].AdvisoryID)
}

func TestFixedAdvisoriesAreTheOnesGoneAfterTheUpdate(t *testing.T) {
	before := composer.Audit{
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

	after := composer.Audit{
		Advisories: []composer.Advisory{
			{
				PackageName: "other/package",
				CVE:         "CVE-2023-5678",
				Title:       "Still exists after update",
			},
		},
	}

	fixed := fixedAdvisories(before, after)

	assert.Len(t, fixed, 1)
	assert.Equal(t, "CVE-2023-1234", fixed[0].CVE)
	assert.Equal(t, "drupal/core", fixed[0].PackageName)
}

// drupal/* is left to unsupported_modules, so one module is not reported twice.
func TestAbandonedPackages_FiltersDrupalPackages(t *testing.T) {
	after := composer.Audit{
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
	}, abandonedPackages(after))
}

// The abandoned list describes the code the merge request contains, not the code it started from.
func TestComposerAuditReportsTheAbandonedPackagesAfterTheUpdate(t *testing.T) {
	ca := auditFor(auditPair{
		before: composer.Audit{Abandoned: []composer.AbandonedPackage{{PackageName: "gone/away"}}},
		after:  composer.Audit{Abandoned: []composer.AbandonedPackage{{PackageName: "still/here"}}},
	})

	data, ok := ca.ReportData().(SecurityAdvisories)
	require.True(t, ok)
	assert.Equal(t, []composer.AbandonedPackage{{PackageName: "still/here"}}, data.Abandoned)
}

func TestComposerAuditComparesTheTwoAuditsThroughThePipeline(t *testing.T) {
	fixed := composer.Advisory{CVE: "CVE-1", PackageName: "drupal/core"}
	open := composer.Advisory{CVE: "CVE-2", PackageName: "drupal/token"}
	mockComposer := NewMockAuditComposer(t)
	mockComposer.EXPECT().Audit(anyCtx, "/p").Return(composer.Audit{Advisories: []composer.Advisory{fixed, open}}, nil).Once()
	mockComposer.EXPECT().Audit(anyCtx, "/p").Return(composer.Audit{Advisories: []composer.Advisory{open}}, nil).Once()
	core, logs := observer.New(zapcore.InfoLevel)
	p := workflow.NewPipeline([]workflow.Addon{NewComposerAudit(zap.New(core), mockComposer, false)})

	_, err := workflow.Settle(p, workflow.NewPreComposerUpdateEvent(t.Context(), "/p", nil, workflow.UpdatePlan{}))
	require.NoError(t, err)
	require.NoError(t, workflow.Fire(p, workflow.NewPostCodeUpdateEvent(t.Context(), "/p", nil)))

	assert.Equal(t, SecurityAdvisories{
		Fixed: []composer.Advisory{fixed}, Remaining: []composer.Advisory{open}, Abandoned: []composer.AbandonedPackage{},
	}, reportSection(p, "composer_audit"))
	require.Equal(t, 1, logs.Len())
	assert.Equal(t, map[string]any{"fixed": int64(1), "unresolved": int64(1), "abandoned": int64(0)}, logs.All()[0].ContextMap())
}

// A normal run audits without taking over: narrowing or aborting here would turn every normal
// run into a security run.
func TestComposerAudit_PreComposerUpdateHandler_NormalMode(t *testing.T) {
	mockComposer := NewMockAuditComposer(t)
	audit := NewComposerAudit(zap.NewNop(), mockComposer, false)
	committer := &fakeCommitter{}

	ctx := context.Background()
	path := "/test/path"
	mockAudit := composer.Audit{
		Advisories: []composer.Advisory{{PackageName: "drupal/core", CVE: "CVE-2023-1234"}},
	}
	mockComposer.EXPECT().Audit(ctx, path).Return(mockAudit, nil)

	mockEvent := workflow.NewPreComposerUpdateEvent(ctx, path, committer, workflow.UpdatePlan{})

	proposal, err := audit.preComposerUpdateHandler(mockEvent)
	require.NoError(t, err)
	assert.Equal(t, mockAudit, proposal.Finding, "the audit still runs: it is what the security report is built from")
	assert.Zero(t, proposal.Plan, "a normal run is not narrowed")
}

// TestComposerAudit_PreComposerUpdateHandler_NormalModeNoAdvisories checks the abort path is
// security-only. On a normal run "no advisories" is the healthy case, not a reason to stop.
func TestComposerAudit_PreComposerUpdateHandler_NormalModeNoAdvisories(t *testing.T) {
	mockComposer := NewMockAuditComposer(t)
	audit := NewComposerAudit(zap.NewNop(), mockComposer, false)
	committer := &fakeCommitter{}

	ctx := context.Background()
	path := "/test/path"
	mockComposer.EXPECT().Audit(ctx, path).Return(composer.Audit{}, nil)

	mockEvent := workflow.NewPreComposerUpdateEvent(ctx, path, committer, workflow.UpdatePlan{})

	proposal, err := audit.preComposerUpdateHandler(mockEvent)
	require.NoError(t, err)
	assert.Zero(t, proposal.Plan)
}

// A swallowed audit failure would let a security run continue with an empty advisory list,
// updating nothing and reporting that everything is fine.
func TestComposerAudit_AuditFailures(t *testing.T) {
	path := "/test/path"

	t.Run("before the update", func(t *testing.T) {
		mockComposer := NewMockAuditComposer(t)
		audit := NewComposerAudit(zap.NewNop(), mockComposer, true)
		mockComposer.EXPECT().Audit(anyCtx, path).Return(composer.Audit{}, assert.AnError)

		evt := workflow.NewPreComposerUpdateEvent(context.Background(), path, &fakeCommitter{}, workflow.UpdatePlan{})
		proposal, err := audit.preComposerUpdateHandler(evt)

		require.ErrorIs(t, err, assert.AnError)
		assert.Contains(t, err.Error(), "failed to run composer audit")
		// Not an abort: aborting reads as "nothing to do here" and exits 0.
		assert.NotErrorAs(t, err, &workflow.AbortError{})
		assert.Zero(t, proposal)
	})

	t.Run("after the update", func(t *testing.T) {
		mockComposer := NewMockAuditComposer(t)
		audit := NewComposerAudit(zap.NewNop(), mockComposer, true)
		mockComposer.EXPECT().Audit(anyCtx, path).Return(composer.Audit{}, assert.AnError)

		evt := workflow.NewPostCodeUpdateEvent(context.Background(), path, &fakeCommitter{})
		_, err := audit.postCodeUpdateHandler(evt, &workflow.Findings{})

		require.ErrorIs(t, err, assert.AnError)
		assert.Contains(t, err.Error(), "failed to run composer audit after update")
	})
}
