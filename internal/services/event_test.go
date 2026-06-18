package services

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAbortError(t *testing.T) {
	err := AbortError{Msg: "something aborted"}
	assert.Equal(t, "something aborted", err.Error())
}

func TestNewPreComposerUpdateEvent(t *testing.T) {
	ctx := context.Background()
	worktree := NewMockWorktree(t)
	evt := NewPreComposerUpdateEvent(ctx, "/repo", worktree, []string{"drupal/core"}, []string{"keep/me"}, true)

	assert.Equal(t, "pre-composer-update", evt.Name())
	assert.Equal(t, ctx, evt.Context())
	assert.Equal(t, "/repo", evt.Path())
	assert.Equal(t, worktree, evt.Worktree())
	assert.Equal(t, []string{"drupal/core"}, evt.PackagesToUpdate)
	assert.Equal(t, []string{"keep/me"}, evt.PackagesToKeep)
	assert.True(t, evt.MinimalChanges)
}

func TestNewPostComposerUpdateEvent(t *testing.T) {
	ctx := context.Background()
	worktree := NewMockWorktree(t)
	evt := NewPostComposerUpdateEvent(ctx, "/repo", worktree)

	assert.Equal(t, "post-composer-update", evt.Name())
	assert.Equal(t, ctx, evt.Context())
	assert.Equal(t, "/repo", evt.Path())
	assert.Equal(t, worktree, evt.Worktree())
}

func TestNewPostCodeUpdateEvent(t *testing.T) {
	ctx := context.Background()
	worktree := NewMockWorktree(t)
	evt := NewPostCodeUpdateEvent(ctx, "/repo", worktree)

	assert.Equal(t, "post-code-update", evt.Name())
	assert.Equal(t, ctx, evt.Context())
	assert.Equal(t, "/repo", evt.Path())
	assert.Equal(t, worktree, evt.Worktree())
}

func TestNewPreSiteUpdateEvent(t *testing.T) {
	ctx := context.Background()
	worktree := NewMockWorktree(t)
	evt := NewPreSiteUpdateEvent(ctx, "/repo", worktree, "default")

	assert.Equal(t, "pre-site-update", evt.Name())
	assert.Equal(t, ctx, evt.Context())
	assert.Equal(t, "/repo", evt.Path())
	assert.Equal(t, worktree, evt.Worktree())
	assert.Equal(t, "default", evt.Site())
}

func TestNewPostSiteUpdateEvent(t *testing.T) {
	ctx := context.Background()
	worktree := NewMockWorktree(t)
	evt := NewPostSiteUpdateEvent(ctx, "/repo", worktree, "production")

	assert.Equal(t, "post-site-update", evt.Name())
	assert.Equal(t, ctx, evt.Context())
	assert.Equal(t, "/repo", evt.Path())
	assert.Equal(t, worktree, evt.Worktree())
	assert.Equal(t, "production", evt.Site())
}

func TestNewPreMergeRequestCreateEvent(t *testing.T) {
	evt := NewPreMergeRequestCreateEvent("June 2025: Drupal Updates")

	assert.Equal(t, "pre-merge-request-create", evt.Name())
	assert.Equal(t, "June 2025: Drupal Updates", evt.Title)
}
