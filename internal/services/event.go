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

func (e *BasicAddonEvent) Context() context.Context {
	return e.ctx
}

func (e *BasicAddonEvent) Path() string {
	return e.path
}

func (e *BasicAddonEvent) Worktree() Worktree {
	return e.worktree
}

// newBasicAddonEvent builds the context every addon event carries.
func newBasicAddonEvent(ctx context.Context, path string, worktree Worktree) BasicAddonEvent {
	return BasicAddonEvent{ctx: ctx, path: path, worktree: worktree}
}

type PreComposerUpdateEvent struct {
	event.BasicEvent
	BasicAddonEvent
	PackagesToUpdate []string
	PackagesToKeep   []string
	MinimalChanges   bool
}

func NewPreComposerUpdateEvent(ctx context.Context, path string, worktree Worktree, packagesToUpdate []string, packagesToKeep []string, minimalChanges bool) *PreComposerUpdateEvent {
	evt := &PreComposerUpdateEvent{
		BasicAddonEvent:  newBasicAddonEvent(ctx, path, worktree),
		PackagesToUpdate: packagesToUpdate,
		PackagesToKeep:   packagesToKeep,
		MinimalChanges:   minimalChanges,
	}
	evt.SetName("pre-composer-update")
	return evt
}

type PostComposerUpdateEvent struct {
	event.BasicEvent
	BasicAddonEvent
}

func NewPostComposerUpdateEvent(ctx context.Context, path string, worktree Worktree) *PostComposerUpdateEvent {
	evt := &PostComposerUpdateEvent{
		BasicAddonEvent: newBasicAddonEvent(ctx, path, worktree),
	}
	evt.SetName("post-composer-update")
	return evt
}

type PostCodeUpdateEvent struct {
	event.BasicEvent
	BasicAddonEvent
}

func NewPostCodeUpdateEvent(ctx context.Context, path string, worktree Worktree) *PostCodeUpdateEvent {
	evt := &PostCodeUpdateEvent{
		BasicAddonEvent: newBasicAddonEvent(ctx, path, worktree),
	}
	evt.SetName("post-code-update")
	return evt
}

type PreSiteUpdateEvent struct {
	event.BasicEvent
	BasicAddonEvent
	site string
}

func NewPreSiteUpdateEvent(ctx context.Context, path string, worktree Worktree, site string) *PreSiteUpdateEvent {
	evt := &PreSiteUpdateEvent{
		BasicAddonEvent: newBasicAddonEvent(ctx, path, worktree),
		site:            site,
	}
	evt.SetName("pre-site-update")
	return evt
}

func (e *PreSiteUpdateEvent) Site() string {
	return e.site
}

type PostSiteUpdateEvent struct {
	event.BasicEvent
	BasicAddonEvent
	site string
}

func NewPostSiteUpdateEvent(ctx context.Context, path string, worktree Worktree, site string) *PostSiteUpdateEvent {
	evt := &PostSiteUpdateEvent{
		BasicAddonEvent: newBasicAddonEvent(ctx, path, worktree),
		site:            site,
	}
	evt.SetName("post-site-update")
	return evt
}

func (e *PostSiteUpdateEvent) Site() string {
	return e.site
}

// AbandonedPackage carries the successor its maintainers suggested, empty when they suggested
// none. It mirrors composer.AbandonedPackage rather than reusing it: this type is the wire
// between two addons, and a change to composer's output shape must not reach through it.
type AbandonedPackage struct {
	Name        string
	Replacement string
}

type PreMergeRequestCreateEvent struct {
	event.BasicEvent
	Title string
	// Written by composer_audit, read by unsupported_modules, which renders both kinds of
	// end-of-life finding as one list — to a reviewer they are the same thing.
	//
	// Both addons' data is complete when this event fires, and the producer subscribes at
	// Normal against the consumer's BelowNormal, so the list is filled in before it is read.
	AbandonedPackages []AbandonedPackage
}

func NewPreMergeRequestCreateEvent(title string) *PreMergeRequestCreateEvent {
	evt := &PreMergeRequestCreateEvent{
		Title: title,
	}
	evt.SetName("pre-merge-request-create")
	return evt
}
