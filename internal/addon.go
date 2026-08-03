package internal

import (
	"bytes"
	"embed"
	"fmt"
	"strings"
	"sync"
	"text/template"

	"github.com/gookit/event"
)

// cellReplacer stops a literal "|" or newline in a value from breaking a markdown table.
var cellReplacer = strings.NewReplacer("|", "\\|", "\n", " ", "\r", "")

//go:embed addon/templates
var templates embed.FS

// Addon is a module that subscribes to workflow events and renders a merge request section.
type Addon interface {
	event.Subscriber

	RenderTemplate() (string, error)
}

type BasicAddon struct {
}

// addonTemplates parses the embedded templates once: the FS is compiled in, so the result is fixed.
var addonTemplates = sync.OnceValues(func() (*template.Template, error) {
	return template.New("").Funcs(template.FuncMap{
		"cell": cellReplacer.Replace,
	}).ParseFS(templates, "addon/templates/*.go.tmpl")
})

func (ba *BasicAddon) Render(name string, data any) (string, error) {
	tmpl, err := addonTemplates()
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
