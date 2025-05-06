package addon

import (
	"context"
	"embed"

	"github.com/drupdater/drupdater/internal"
	"github.com/gookit/event"
)

// templates contains embedded template files for addons
//
//go:embed */templates
var templates embed.FS

type BasicAddonEvent struct {
	ctx      context.Context
	path     string
	worktree internal.Worktree
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
func (e *BasicAddonEvent) Worktree() internal.Worktree {
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
func NewPreComposerUpdateEvent(ctx context.Context, path string, worktree internal.Worktree, packagesToUpdate []string, packagesToKeep []string, minimalChanges bool) *PreComposerUpdateEvent {
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
func NewPostComposerUpdateEvent(ctx context.Context, path string, worktree internal.Worktree) *PostComposerUpdateEvent {
	evt := &PostComposerUpdateEvent{
		BasicAddonEvent: BasicAddonEvent{
			ctx:      ctx,
			path:     path,
			worktree: worktree,
		},
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
func NewPostCodeUpdateEvent(ctx context.Context, path string, worktree internal.Worktree) *PostCodeUpdateEvent {
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

// PostSiteUpdateEvent is triggered after site update operations
type PostSiteUpdateEvent struct {
	event.BasicEvent
	BasicAddonEvent
	site string
}

// NewPostSiteUpdateEvent creates a new PostSiteUpdateEvent instance
func NewPostSiteUpdateEvent(ctx context.Context, path string, worktree internal.Worktree, site string) *PostSiteUpdateEvent {
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
