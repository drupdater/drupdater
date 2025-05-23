package services

import (
	"context"

	"github.com/gookit/event"
)

type AbortError struct {
	Msg string
}

func (e AbortError) Error() string {
	return e.Msg
}

type BasicAddonEvent struct {
	ctx      context.Context
	path     string
	worktree Worktree
}

// Context returns the context
func (e *BasicAddonEvent) Context() context.Context {
	return e.ctx
}

// Path returns the file path
func (e *BasicAddonEvent) Path() string {
	return e.path
}

// Worktree returns the worktree
func (e *BasicAddonEvent) Worktree() Worktree {
	return e.worktree
}

// PreComposerUpdateEvent is triggered before composer update operations
type PreComposerUpdateEvent struct {
	event.BasicEvent
	BasicAddonEvent
	PackagesToUpdate []string
	PackagesToKeep   []string
	MinimalChanges   bool
}

// NewPreComposerUpdateEvent creates a new PreComposerUpdateEvent instance
func NewPreComposerUpdateEvent(ctx context.Context, path string, worktree Worktree, packagesToUpdate []string, packagesToKeep []string, minimalChanges bool) *PreComposerUpdateEvent {
	evt := &PreComposerUpdateEvent{
		BasicAddonEvent: BasicAddonEvent{
			ctx:      ctx,
			path:     path,
			worktree: worktree,
		},
		PackagesToUpdate: packagesToUpdate,
		PackagesToKeep:   packagesToKeep,
		MinimalChanges:   minimalChanges,
	}
	evt.SetName("pre-composer-update")
	return evt
}

// PostComposerUpdateEvent is triggered after composer update operations
type PostComposerUpdateEvent struct {
	event.BasicEvent
	BasicAddonEvent
}

// NewPostComposerUpdateEvent creates a new PostComposerUpdateEvent instance
func NewPostComposerUpdateEvent(ctx context.Context, path string, worktree Worktree) *PostComposerUpdateEvent {
	evt := &PostComposerUpdateEvent{
		BasicAddonEvent: BasicAddonEvent{
			ctx:      ctx,
			path:     path,
			worktree: worktree},
	}
	evt.SetName("post-composer-update")
	return evt
}

// PostCodeUpdateEvent is triggered after code update operations
type PostCodeUpdateEvent struct {
	event.BasicEvent
	BasicAddonEvent
}

// NewPostCodeUpdateEvent creates a new PostCodeUpdateEvent instance
func NewPostCodeUpdateEvent(ctx context.Context, path string, worktree Worktree) *PostCodeUpdateEvent {
	evt := &PostCodeUpdateEvent{
		BasicAddonEvent: BasicAddonEvent{
			ctx:      ctx,
			path:     path,
			worktree: worktree,
		},
	}
	evt.SetName("post-code-update")
	return evt
}

// PreSiteUpdateEvent is triggered after site update operations
type PreSiteUpdateEvent struct {
	event.BasicEvent
	BasicAddonEvent
	site string
}

// NewPostSiteUpdateEvent creates a new PreSiteUpdateEvent instance
func NewPreSiteUpdateEvent(ctx context.Context, path string, worktree Worktree, site string) *PreSiteUpdateEvent {
	evt := &PreSiteUpdateEvent{
		BasicAddonEvent: BasicAddonEvent{
			ctx:      ctx,
			path:     path,
			worktree: worktree,
		},
		site: site,
	}
	evt.SetName("pre-site-update")
	return evt
}

// Site returns the site name
func (e *PreSiteUpdateEvent) Site() string {
	return e.site
}

// PostSiteUpdateEvent is triggered after site update operations
type PostSiteUpdateEvent struct {
	event.BasicEvent
	BasicAddonEvent
	site string
}

// NewPostSiteUpdateEvent creates a new PostSiteUpdateEvent instance
func NewPostSiteUpdateEvent(ctx context.Context, path string, worktree Worktree, site string) *PostSiteUpdateEvent {
	evt := &PostSiteUpdateEvent{
		BasicAddonEvent: BasicAddonEvent{
			ctx:      ctx,
			path:     path,
			worktree: worktree,
		},
		site: site,
	}
	evt.SetName("post-site-update")
	return evt
}

// Site returns the site name
func (e *PostSiteUpdateEvent) Site() string {
	return e.site
}

type PreMergeRequestCreateEvent struct {
	event.BasicEvent
	Title string
}

// NewPreMergeRequestCreateEvent creates a new PreMergeRequestCreateEvent instance
func NewPreMergeRequestCreateEvent(title string) *PreMergeRequestCreateEvent {
	evt := &PreMergeRequestCreateEvent{
		Title: title,
	}
	evt.SetName("pre-merge-request-create")
	return evt
}
