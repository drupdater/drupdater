package workflow

import (
	"context"

	"github.com/drupdater/drupdater/internal/drush"
)

// The events' names, which a Reporter passes to Sites to read what its hooks found.
const (
	EventPreComposerUpdate  = "pre-composer-update"
	EventPostComposerUpdate = "post-composer-update"
	EventPostCodeUpdate     = "post-code-update"
	EventPreSiteUpdate      = "pre-site-update"
	EventPostSiteUpdate     = "post-site-update"
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

func (e *BasicAddonEvent) commit(message string, paths ...string) (bool, error) {
	return e.committer.Commit(e.ctx, message, paths...)
}

// newBasicAddonEvent builds the context every addon event carries.
func newBasicAddonEvent(ctx context.Context, path string, committer Committer) BasicAddonEvent {
	return BasicAddonEvent{ctx: ctx, path: path, committer: committer}
}

type PreComposerUpdateEvent struct {
	BasicAddonEvent
	plan UpdatePlan
}

func (*PreComposerUpdateEvent) name() string { return EventPreComposerUpdate }

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

func (*PostComposerUpdateEvent) name() string { return EventPostComposerUpdate }

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

func (*PostCodeUpdateEvent) name() string { return EventPostCodeUpdate }

func (*PostCodeUpdateEvent) hook(h Hooks) *Hook[*PostCodeUpdateEvent] { return h.PostCodeUpdate }

func NewPostCodeUpdateEvent(ctx context.Context, path string, committer Committer) *PostCodeUpdateEvent {
	return &PostCodeUpdateEvent{
		BasicAddonEvent: newBasicAddonEvent(ctx, path, committer),
	}
}

type PreSiteUpdateEvent struct {
	BasicAddonEvent
	site  string
	facts drush.Facts
}

func (*PreSiteUpdateEvent) name() string { return EventPreSiteUpdate }

func (*PreSiteUpdateEvent) hook(h Hooks) *Hook[*PreSiteUpdateEvent] { return h.PreSiteUpdate }

func NewPreSiteUpdateEvent(ctx context.Context, path string, committer Committer, site string, facts drush.Facts) *PreSiteUpdateEvent {
	return &PreSiteUpdateEvent{
		BasicAddonEvent: newBasicAddonEvent(ctx, path, committer),
		site:            site,
		facts:           facts,
	}
}

func (e *PreSiteUpdateEvent) Site() string {
	return e.site
}

// Facts is what the site reported before its update hooks ran.
func (e *PreSiteUpdateEvent) Facts() drush.Facts {
	return e.facts.Clone()
}

type PostSiteUpdateEvent struct {
	BasicAddonEvent
	site  string
	facts drush.Facts
}

func (*PostSiteUpdateEvent) name() string { return EventPostSiteUpdate }

func (*PostSiteUpdateEvent) hook(h Hooks) *Hook[*PostSiteUpdateEvent] { return h.PostSiteUpdate }

func NewPostSiteUpdateEvent(ctx context.Context, path string, committer Committer, site string, facts drush.Facts) *PostSiteUpdateEvent {
	return &PostSiteUpdateEvent{
		BasicAddonEvent: newBasicAddonEvent(ctx, path, committer),
		site:            site,
		facts:           facts,
	}
}

func (e *PostSiteUpdateEvent) Site() string {
	return e.site
}

// Facts is what the site reported after its update hooks ran, without UnsupportedModules.
func (e *PostSiteUpdateEvent) Facts() drush.Facts {
	return e.facts.Clone()
}
