package addon

import (
	"context"
	"testing"

	"github.com/drupdater/drupdater/internal/workflow"
	"github.com/drupdater/drupdater/pkg/composer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// TestNewComposerAudit tests the constructor
func TestNewComposerAudit(t *testing.T) {
	logger := zap.NewNop()
	mockComposer := NewMockComposer(t)

	audit := NewComposerAudit(logger, mockComposer, true)

	assert.NotNil(t, audit)
	assert.Equal(t, logger, audit.logger)
	assert.Equal(t, mockComposer, audit.composer)
	assert.True(t, audit.security)
}

// TestComposerAudit_PreComposerUpdateHandler_WithAdvisories tests handling security advisories
func TestComposerAudit_PreComposerUpdateHandler_WithAdvisories(t *testing.T) {
	logger := zap.NewNop()
	mockComposer := NewMockComposer(t)
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

	plan, err := audit.preComposerUpdateHandler(mockEvent)

	require.NoError(t, err)
	assert.Equal(t, mockAudit, audit.beforeAudit)
	assert.Equal(t, workflow.UpdatePlan{
		Only:           []string{"drupal/core", "other/package", "drupal/core-recommended", "drupal/core-composer-scaffold"},
		MinimalChanges: true,
	}, plan)
}

// TestComposerAudit_PreComposerUpdateHandler_NoAdvisories tests when no security advisories are found
func TestComposerAudit_PreComposerUpdateHandler_NoAdvisories(t *testing.T) {
	logger := zap.NewNop()
	mockComposer := NewMockComposer(t)
	audit := NewComposerAudit(logger, mockComposer, true)
	committer := &fakeCommitter{}

	ctx := context.Background()
	path := "/test/path"

	mockAudit := composer.Audit{
		Advisories: []composer.Advisory{},
	}

	mockEvent := workflow.NewPreComposerUpdateEvent(ctx, path, committer, workflow.UpdatePlan{})

	mockComposer.EXPECT().Audit(ctx, path).Return(mockAudit, nil)

	plan, err := audit.preComposerUpdateHandler(mockEvent)

	require.ErrorAs(t, err, &workflow.AbortError{})
	assert.Equal(t, "No security advisories found", err.Error())
	assert.Equal(t, mockAudit, audit.beforeAudit)
	assert.Zero(t, plan)
}

// TestComposerAudit_PostCodeUpdateHandler tests the post-code-update handler
func TestComposerAudit_PostCodeUpdateHandler(t *testing.T) {
	logger := zap.NewNop()
	mockComposer := NewMockComposer(t)
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
	committer := &fakeCommitter{}

	ctx := context.Background()
	path := "/test/path"
	mockAudit := composer.Audit{
		Advisories: []composer.Advisory{{PackageName: "drupal/core", CVE: "CVE-2023-1234"}},
	}
	mockComposer.EXPECT().Audit(ctx, path).Return(mockAudit, nil)

	mockEvent := workflow.NewPreComposerUpdateEvent(ctx, path, committer, workflow.UpdatePlan{})

	plan, err := audit.preComposerUpdateHandler(mockEvent)
	require.NoError(t, err)
	assert.Equal(t, mockAudit, audit.beforeAudit, "the audit still runs: it is what the security report is built from")
	assert.Zero(t, plan, "a normal run is not narrowed")
}

// TestComposerAudit_PreComposerUpdateHandler_NormalModeNoAdvisories checks the abort path is
// security-only. On a normal run "no advisories" is the healthy case, not a reason to stop.
func TestComposerAudit_PreComposerUpdateHandler_NormalModeNoAdvisories(t *testing.T) {
	mockComposer := NewMockComposer(t)
	audit := NewComposerAudit(zap.NewNop(), mockComposer, false)
	committer := &fakeCommitter{}

	ctx := context.Background()
	path := "/test/path"
	mockComposer.EXPECT().Audit(ctx, path).Return(composer.Audit{}, nil)

	mockEvent := workflow.NewPreComposerUpdateEvent(ctx, path, committer, workflow.UpdatePlan{})

	plan, err := audit.preComposerUpdateHandler(mockEvent)
	require.NoError(t, err)
	assert.Zero(t, plan)
}

// A swallowed audit failure would let a security run continue with an empty advisory list,
// updating nothing and reporting that everything is fine.
func TestComposerAudit_AuditFailures(t *testing.T) {
	path := "/test/path"

	t.Run("before the update", func(t *testing.T) {
		mockComposer := NewMockComposer(t)
		audit := NewComposerAudit(zap.NewNop(), mockComposer, true)
		mockComposer.EXPECT().Audit(anyCtx, path).Return(composer.Audit{}, assert.AnError)

		evt := workflow.NewPreComposerUpdateEvent(context.Background(), path, &fakeCommitter{}, workflow.UpdatePlan{})
		plan, err := audit.preComposerUpdateHandler(evt)

		require.ErrorIs(t, err, assert.AnError)
		assert.Contains(t, err.Error(), "failed to run composer audit")
		// Not an abort: aborting reads as "nothing to do here" and exits 0.
		assert.NotErrorAs(t, err, &workflow.AbortError{})
		assert.Zero(t, plan)
	})

	t.Run("after the update", func(t *testing.T) {
		mockComposer := NewMockComposer(t)
		audit := NewComposerAudit(zap.NewNop(), mockComposer, true)
		mockComposer.EXPECT().Audit(anyCtx, path).Return(composer.Audit{}, assert.AnError)

		evt := workflow.NewPostCodeUpdateEvent(context.Background(), path, &fakeCommitter{})
		err := audit.postCodeUpdateHandler(evt)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to run composer audit after update")
	})
}
