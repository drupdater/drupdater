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

// cellReplacer escapes values interpolated into markdown table cells so a
// literal "|" or newline can't break the table layout.
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

// addonTemplates parses the embedded section templates once. The FS is compiled in, so the
// result — success or failure — is the same on every call.
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
