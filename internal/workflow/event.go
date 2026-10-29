package workflow

import "context"

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
	BasicAddonEvent
	PackagesToUpdate []string
	PackagesToKeep   []string
	MinimalChanges   bool
}

func NewPreComposerUpdateEvent(ctx context.Context, path string, committer Committer, packagesToUpdate []string, packagesToKeep []string, minimalChanges bool) *PreComposerUpdateEvent {
	return &PreComposerUpdateEvent{
		BasicAddonEvent:  newBasicAddonEvent(ctx, path, committer),
		PackagesToUpdate: packagesToUpdate,
		PackagesToKeep:   packagesToKeep,
		MinimalChanges:   minimalChanges,
	}
}

type PostComposerUpdateEvent struct {
	BasicAddonEvent
}

func NewPostComposerUpdateEvent(ctx context.Context, path string, committer Committer) *PostComposerUpdateEvent {
	return &PostComposerUpdateEvent{
		BasicAddonEvent: newBasicAddonEvent(ctx, path, committer),
	}
}

type PostCodeUpdateEvent struct {
	BasicAddonEvent
}

func NewPostCodeUpdateEvent(ctx context.Context, path string, committer Committer) *PostCodeUpdateEvent {
	return &PostCodeUpdateEvent{
		BasicAddonEvent: newBasicAddonEvent(ctx, path, committer),
	}
}

type PreSiteUpdateEvent struct {
	BasicAddonEvent
	site string
}

func NewPreSiteUpdateEvent(ctx context.Context, path string, committer Committer, site string) *PreSiteUpdateEvent {
	return &PreSiteUpdateEvent{
		BasicAddonEvent: newBasicAddonEvent(ctx, path, committer),
		site:            site,
	}
}

func (e *PreSiteUpdateEvent) Site() string {
	return e.site
}

type PostSiteUpdateEvent struct {
	BasicAddonEvent
	site string
}

func NewPostSiteUpdateEvent(ctx context.Context, path string, committer Committer, site string) *PostSiteUpdateEvent {
	return &PostSiteUpdateEvent{
		BasicAddonEvent: newBasicAddonEvent(ctx, path, committer),
		site:            site,
	}
}

func (e *PostSiteUpdateEvent) Site() string {
	return e.site
}
