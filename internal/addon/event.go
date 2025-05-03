package addon

import (
	"context"
	"embed"

	"github.com/drupdater/drupdater/internal"
	"github.com/gookit/event"
)

//go:embed */templates
var templates embed.FS

type PostCodeUpdateEvent struct {
	event.BasicEvent
	Ctx      context.Context
	Path     string
	Worktree internal.Worktree
}

type PreComposerUpdateEvent struct {
	event.BasicEvent
	Ctx      context.Context
	Path     string
	Worktree internal.Worktree
}

type PostComposerUpdateEvent struct {
	event.BasicEvent
	Ctx      context.Context
	Path     string
	Worktree internal.Worktree
}

type PostSiteUpdateEvent struct {
	event.BasicEvent
	Ctx      context.Context
	Path     string
	Worktree internal.Worktree
	Site     string
}
