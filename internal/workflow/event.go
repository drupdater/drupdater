package workflow

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
	ctx       context.Context
	path      string
	committer Committer
}

func (e *BasicAddonEvent) Context() context.Context {
	return e.ctx
}

func (e *BasicAddonEvent) Path() string {
	return e.path
}

// Commit records the addon's change to paths on the update branch; see Committer.
func (e *BasicAddonEvent) Commit(message string, paths ...string) (bool, error) {
	return e.committer.Commit(message, paths...)
}

// newBasicAddonEvent builds the context every addon event carries.
func newBasicAddonEvent(ctx context.Context, path string, committer Committer) BasicAddonEvent {
	return BasicAddonEvent{ctx: ctx, path: path, committer: committer}
}

type PreComposerUpdateEvent struct {
	event.BasicEvent
	BasicAddonEvent
	PackagesToUpdate []string
	PackagesToKeep   []string
	MinimalChanges   bool
}

func NewPreComposerUpdateEvent(ctx context.Context, path string, committer Committer, packagesToUpdate []string, packagesToKeep []string, minimalChanges bool) *PreComposerUpdateEvent {
	evt := &PreComposerUpdateEvent{
		BasicAddonEvent:  newBasicAddonEvent(ctx, path, committer),
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

func NewPostComposerUpdateEvent(ctx context.Context, path string, committer Committer) *PostComposerUpdateEvent {
	evt := &PostComposerUpdateEvent{
		BasicAddonEvent: newBasicAddonEvent(ctx, path, committer),
	}
	evt.SetName("post-composer-update")
	return evt
}

type PostCodeUpdateEvent struct {
	event.BasicEvent
	BasicAddonEvent
}

func NewPostCodeUpdateEvent(ctx context.Context, path string, committer Committer) *PostCodeUpdateEvent {
	evt := &PostCodeUpdateEvent{
		BasicAddonEvent: newBasicAddonEvent(ctx, path, committer),
	}
	evt.SetName("post-code-update")
	return evt
}

type PreSiteUpdateEvent struct {
	event.BasicEvent
	BasicAddonEvent
	site string
}

func NewPreSiteUpdateEvent(ctx context.Context, path string, committer Committer, site string) *PreSiteUpdateEvent {
	evt := &PreSiteUpdateEvent{
		BasicAddonEvent: newBasicAddonEvent(ctx, path, committer),
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

func NewPostSiteUpdateEvent(ctx context.Context, path string, committer Committer, site string) *PostSiteUpdateEvent {
	evt := &PostSiteUpdateEvent{
		BasicAddonEvent: newBasicAddonEvent(ctx, path, committer),
		site:            site,
	}
	evt.SetName("post-site-update")
	return evt
}

func (e *PostSiteUpdateEvent) Site() string {
	return e.site
}

// AbandonedPackage mirrors composer.AbandonedPackage: it is the wire between two addons, so a
// change to composer's output shape must not reach through it. Replacement is "" when none.
type AbandonedPackage struct {
	Name        string
	Replacement string
}

type PreMergeRequestCreateEvent struct {
	event.BasicEvent
	Title string
	// Written by composer_audit at Normal, read by unsupported_modules at BelowNormal — that
	// priority gap is what fills the list before it is read.
	AbandonedPackages []AbandonedPackage
}

func NewPreMergeRequestCreateEvent(title string) *PreMergeRequestCreateEvent {
	evt := &PreMergeRequestCreateEvent{
		Title: title,
	}
	evt.SetName("pre-merge-request-create")
	return evt
}
