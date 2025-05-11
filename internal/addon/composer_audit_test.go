package addon

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/drupdater/drupdater/internal/services"
	"github.com/drupdater/drupdater/pkg/composer"
	"github.com/gookit/event"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

// TestNewComposerAudit tests the constructor
func TestNewComposerAudit(t *testing.T) {
	// Setup
	logger := zap.NewNop()
	mockComposer := NewMockComposer(t)

	// Execute
	before := time.Now()
	audit := NewComposerAudit(logger, mockComposer)
	after := time.Now()

	// Assert
	assert.NotNil(t, audit)
	assert.Equal(t, logger, audit.logger)
	assert.Equal(t, mockComposer, audit.composer)
	assert.True(t, audit.current.After(before) || audit.current.Equal(before))
	assert.True(t, audit.current.Before(after) || audit.current.Equal(after))
}

// TestComposerAudit_SubscribedEvents tests the event subscription
func TestComposerAudit_SubscribedEvents(t *testing.T) {
	// Setup
	audit := &ComposerAudit{}

	// Execute
	events := audit.SubscribedEvents()

	// Assert
	assert.Contains(t, events, "pre-composer-update")
	assert.Contains(t, events, "post-code-update")
	assert.Contains(t, events, "pre-merge-request-create")

	preComposerEvent := events["pre-composer-update"].(event.ListenerItem)
	assert.Equal(t, event.Max, preComposerEvent.Priority)

	postCodeEvent := events["post-code-update"].(event.ListenerItem)
	assert.Equal(t, event.Normal, postCodeEvent.Priority)

	preMergeEvent := events["pre-merge-request-create"].(event.ListenerItem)
	assert.Equal(t, event.Normal, preMergeEvent.Priority)
}

// TestComposerAudit_PreComposerUpdateHandler_WithAdvisories tests handling security advisories
func TestComposerAudit_PreComposerUpdateHandler_WithAdvisories(t *testing.T) {
	// Setup
	logger := zap.NewNop()
	mockComposer := NewMockComposer(t)
	audit := NewComposerAudit(logger, mockComposer)
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

	mockComposer.On("Audit", ctx, path).Return(mockAudit, nil)

	// Execute
	err := audit.preComposerUpdateHandler(mockEvent)

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, mockAudit, audit.beforeAudit)
	assert.Equal(t, []string{"drupal/core", "other/package", "drupal/core-recommended", "drupal/core-composer-scaffold"}, mockEvent.PackagesToUpdate)
	assert.True(t, mockEvent.MinimalChanges)
	assert.False(t, mockEvent.IsAborted())

	mockComposer.AssertExpectations(t)
}

// TestComposerAudit_PreComposerUpdateHandler_NoAdvisories tests when no security advisories are found
func TestComposerAudit_PreComposerUpdateHandler_NoAdvisories(t *testing.T) {
	// Setup
	logger := zap.NewNop()
	mockComposer := NewMockComposer(t)
	audit := NewComposerAudit(logger, mockComposer)
	worktree := NewMockWorktree(t)

	ctx := context.Background()
	path := "/test/path"

	mockAudit := composer.Audit{
		Advisories: []composer.Advisory{},
	}

	mockEvent := services.NewPreComposerUpdateEvent(ctx, path, worktree, []string{}, []string{}, true)

	mockComposer.On("Audit", ctx, path).Return(mockAudit, nil)

	// Execute
	err := audit.preComposerUpdateHandler(mockEvent)

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, mockAudit, audit.beforeAudit)
	assert.Empty(t, mockEvent.PackagesToUpdate)

	mockComposer.AssertExpectations(t)
}

// TestComposerAudit_PostCodeUpdateHandler tests the post-code-update handler
func TestComposerAudit_PostCodeUpdateHandler(t *testing.T) {
	// Setup
	logger := zap.NewNop()
	mockComposer := NewMockComposer(t)
	worktree := NewMockWorktree(t)
	audit := NewComposerAudit(logger, mockComposer)

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

	mockComposer.On("Audit", ctx, path).Return(mockAudit, nil)

	// Execute
	err := audit.postCodeUpdateHandler(mockEvent)

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, mockAudit, audit.afterAudit)

	mockComposer.AssertExpectations(t)
}

// TestComposerAudit_GetFixedAdvisories tests the method that compares before/after advisories
func TestComposerAudit_GetFixedAdvisories(t *testing.T) {
	// Setup
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

	// Execute
	fixed := audit.GetFixedAdvisories()

	// Assert
	assert.Len(t, fixed, 1)
	assert.Equal(t, "CVE-2023-1234", fixed[0].CVE)
	assert.Equal(t, "drupal/core", fixed[0].PackageName)
}

// TestComposerAudit_PreMergeRequestCreateHandler tests the merge request title generation
func TestComposerAudit_PreMergeRequestCreateHandler(t *testing.T) {
	// Setup
	fixedDate := time.Date(2023, 5, 15, 12, 0, 0, 0, time.UTC)
	audit := &ComposerAudit{
		current: fixedDate,
	}

	mockEvent := &services.PreMergeRequestCreateEvent{}
	mockEvent.SetName("pre-merge-request-create")

	// Execute
	err := audit.preMergeRequestCreateHandler(mockEvent)

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, "2023-05-15: Drupal Security Updates", mockEvent.Title)
}

// TestComposerAudit_RenderTemplate tests template rendering
func TestComposerAudit_RenderTemplate(t *testing.T) {
	logger := zap.NewNop()
	mockComposer := NewMockComposer(t)
	audit := NewComposerAudit(logger, mockComposer)

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

	fixture, _ := os.ReadFile("testdata/composer_audit.md")
	expected := string(fixture)

	result, err := audit.RenderTemplate()
	assert.NoError(t, err)
	assert.Equal(t, expected, result)
	mockComposer.AssertExpectations(t)
}
