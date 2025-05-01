package addon

import (
	"context"

	"github.com/drupdater/drupdater/internal"
	"github.com/gookit/event"
)

type PostCodeUpdate struct {
	event.BasicEvent
	Ctx      context.Context
	Path     string
	Worktree internal.Worktree
}

type PostSiteUpdate struct {
	event.BasicEvent
	Ctx      context.Context
	Path     string
	Worktree internal.Worktree
	Site     string
}
