package internal

import (
	"bytes"
	"embed"
	"fmt"
	"strings"
	"text/template"

	"github.com/gookit/event"
)

// cellReplacer escapes values interpolated into markdown table cells so a
// literal "|" or newline can't break the table layout.
var cellReplacer = strings.NewReplacer("|", "\\|", "\n", " ", "\r", "")

// codeReplacer neutralises values interpolated into an inline markdown code span. A backtick
// closes the span early and a newline ends it outright, so either would let a value — some of
// which, such as a package's suggested replacement, are free text written by a third-party
// maintainer — break out of the span and inject markdown into the merge request description.
var codeReplacer = strings.NewReplacer("`", "'", "\n", " ", "\r", "")

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
	tmpl, err := template.New("").Funcs(template.FuncMap{
		"cell": cellReplacer.Replace,
		"code": codeReplacer.Replace,
	}).ParseFS(templates, "addon/templates/*.go.tmpl")
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
