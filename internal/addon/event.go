package addon

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"text/template"

	"github.com/drupdater/drupdater/internal"
	"github.com/gookit/event"
)

type Addon interface {
	event.Subscriber

	RenderTemplate() (string, error)
}

//go:embed */templates
var templates embed.FS

type BasicAddon struct {
}

func (h *BasicAddon) Render(name string, data any) (string, error) {
	tmpl, err := template.ParseFS(templates, "templates/*.go.tmpl")
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %w", err)
	}

	var output bytes.Buffer

	err = tmpl.ExecuteTemplate(&output, name, data)
	if err != nil {
		return "", fmt.Errorf("failed to execute template: %w", err)
	}

	return output.String(), nil
}

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
