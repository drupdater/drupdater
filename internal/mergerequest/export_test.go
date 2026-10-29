package mergerequest

import (
	"text/template"

	"github.com/drupdater/drupdater/internal/report"
)

// DescribeWith renders rep with tmpl in place of the embedded templates.
func DescribeWith(tmpl *template.Template, rep report.Report) (string, error) {
	return describe(tmpl, rep)
}

// RenderedSections returns each section the description of rep holds, by template name, in order.
func RenderedSections(rep report.Report) (names []string, sections []string, err error) {
	s, err := readSections(rep)
	if err != nil {
		return nil, nil, err
	}
	tmpl, err := parsedTemplates()
	if err != nil {
		return nil, nil, err
	}
	for _, sec := range s.sectionsInOrder(true) {
		out, err := renderSection(tmpl, sec)
		if err != nil {
			return nil, nil, err
		}
		names = append(names, sec.template)
		sections = append(sections, out)
	}
	return names, sections, nil
}
