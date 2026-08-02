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

// AbandonedPackage names a package whose maintainers have marked it abandoned, together with
// the successor they suggested — empty when they suggested none.
//
// It mirrors composer.AbandonedPackage rather than reusing it. This type is the wire between
// two addons, and the workflow has no business depending on how one of them happens to obtain
// its data; a change to composer's output shape must not reach through to the other side.
type AbandonedPackage struct {
	Name        string
	Replacement string
}

type PreMergeRequestCreateEvent struct {
	event.BasicEvent
	Title string
	// AbandonedPackages is contributed by composer_audit and consumed by unsupported_modules,
	// which renders both kinds of end-of-life finding as one list. An abandoned package and an
	// unsupported module are the same thing to a reviewer — something that will get no further
	// fixes and needs a decision — so splitting them across two sections of the same merge
	// request would be an artefact of which addon happened to find them.
	//
	// This works because both addons' data is complete by the time this event fires, and the
	// description is rendered after it. The producer subscribes at Normal and the consumer at
	// BelowNormal, so the list is filled in before it is read.
	AbandonedPackages []AbandonedPackage
}

// NewPreMergeRequestCreateEvent creates a new PreMergeRequestCreateEvent instance
func NewPreMergeRequestCreateEvent(title string) *PreMergeRequestCreateEvent {
	evt := &PreMergeRequestCreateEvent{
		Title: title,
	}
	evt.SetName("pre-merge-request-create")
	return evt
}
