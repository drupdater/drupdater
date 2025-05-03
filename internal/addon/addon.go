package addon

import (
	"bytes"
	"fmt"
	"html/template"

	"github.com/gookit/event"
)

type Addon interface {
	event.Subscriber

	RenderTemplate() (string, error)
}

type BasicAddon struct {
}

func (h *BasicAddon) Render(name string, data any) (string, error) {
	tmpl, err := template.ParseFS(templates, "*/templates/*.go.tmpl")
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
