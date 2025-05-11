package internal

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"

	"github.com/gookit/event"
)

// templates contains embedded template files for addons
//
//go:embed addon/templates
var templates embed.FS

// Addon represents a module that can subscribe to events and render templates
type Addon interface {
	event.Subscriber

	// RenderTemplate renders the addon's template and returns the result
	RenderTemplate() (string, error)
}

// BasicAddon provides common functionality for addons
type BasicAddon struct {
}

// Render renders a template with the given name and data
func (ba *BasicAddon) Render(name string, data any) (string, error) {
	tmpl, err := template.ParseFS(templates, "addon/templates/*.go.tmpl")
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
