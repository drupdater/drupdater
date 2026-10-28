package addon

import (
	"bytes"
	"embed"
	"fmt"
	"strings"
	"sync"
	"text/template"
)

// cellReplacer stops a literal "|" or newline in a value from breaking a markdown table.
var cellReplacer = strings.NewReplacer("|", "\\|", "\n", " ", "\r", "")

//go:embed templates
var templates embed.FS

// parsedTemplates parses the embedded templates once: the FS is compiled in, so the result is fixed.
var parsedTemplates = sync.OnceValues(func() (*template.Template, error) {
	return template.New("").Funcs(template.FuncMap{
		"cell": cellReplacer.Replace,
	}).ParseFS(templates, "templates/*.go.tmpl")
})

func render(name string, data any) (string, error) {
	tmpl, err := parsedTemplates()
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %w", err)
	}

	var output bytes.Buffer
	if err := tmpl.ExecuteTemplate(&output, name, data); err != nil {
		return "", fmt.Errorf("failed to execute template: %w", err)
	}

	return output.String(), nil
}
