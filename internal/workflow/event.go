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
	plan UpdatePlan
}

func (*PreComposerUpdateEvent) name() string { return "pre-composer-update" }

func (*PreComposerUpdateEvent) hook(h Hooks) *PlanHook { return h.PreComposerUpdate }

func NewPreComposerUpdateEvent(ctx context.Context, path string, committer Committer, plan UpdatePlan) *PreComposerUpdateEvent {
	return &PreComposerUpdateEvent{
		BasicAddonEvent: newBasicAddonEvent(ctx, path, committer),
		plan:            plan,
	}
}

// Plan is the update as the event's earlier stages settled it. A hook proposes changes to it by
// returning them, never by editing this copy.
func (e *PreComposerUpdateEvent) Plan() UpdatePlan {
	return e.plan.clone()
}

type PostComposerUpdateEvent struct {
	BasicAddonEvent
}

func (*PostComposerUpdateEvent) name() string { return "post-composer-update" }

func (*PostComposerUpdateEvent) hook(h Hooks) *Hook[*PostComposerUpdateEvent] {
	return h.PostComposerUpdate
}

func NewPostComposerUpdateEvent(ctx context.Context, path string, committer Committer) *PostComposerUpdateEvent {
	return &PostComposerUpdateEvent{
		BasicAddonEvent: newBasicAddonEvent(ctx, path, committer),
	}
}

type PostCodeUpdateEvent struct {
	BasicAddonEvent
}

func (*PostCodeUpdateEvent) name() string { return "post-code-update" }

func (*PostCodeUpdateEvent) hook(h Hooks) *Hook[*PostCodeUpdateEvent] { return h.PostCodeUpdate }

func NewPostCodeUpdateEvent(ctx context.Context, path string, committer Committer) *PostCodeUpdateEvent {
	return &PostCodeUpdateEvent{
		BasicAddonEvent: newBasicAddonEvent(ctx, path, committer),
	}
}

type PreSiteUpdateEvent struct {
	BasicAddonEvent
	site string
}

func (*PreSiteUpdateEvent) name() string { return "pre-site-update" }

func (*PreSiteUpdateEvent) hook(h Hooks) *Hook[*PreSiteUpdateEvent] { return h.PreSiteUpdate }

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

func (*PostSiteUpdateEvent) name() string { return "post-site-update" }

func (*PostSiteUpdateEvent) hook(h Hooks) *Hook[*PostSiteUpdateEvent] { return h.PostSiteUpdate }

func NewPostSiteUpdateEvent(ctx context.Context, path string, committer Committer, site string) *PostSiteUpdateEvent {
	return &PostSiteUpdateEvent{
		BasicAddonEvent: newBasicAddonEvent(ctx, path, committer),
		site:            site,
	}
}

func (e *PostSiteUpdateEvent) Site() string {
	return e.site
}
