package addon

import (
	"testing"

	composer "github.com/drupdater/drupdater/pkg/composer"
	"github.com/stretchr/testify/assert"
)

func TestGetFixedAdvisories(t *testing.T) {
	tests := []struct {
		name     string
		before   []composer.Advisory
		after    []composer.Advisory
		expected []composer.Advisory
	}{
		{
			name: "No advisories fixed",
			before: []composer.Advisory{
				{CVE: "CVE-1234", Title: "Vulnerability 1"},
				{CVE: "CVE-5678", Title: "Vulnerability 2"},
			},
			after: []composer.Advisory{
				{CVE: "CVE-1234", Title: "Vulnerability 1"},
				{CVE: "CVE-5678", Title: "Vulnerability 2"},
			},
			expected: []composer.Advisory{},
		},
		{
			name: "Some advisories fixed",
			before: []composer.Advisory{
				{CVE: "CVE-1234", Title: "Vulnerability 1"},
				{CVE: "CVE-5678", Title: "Vulnerability 2"},
			},
			after: []composer.Advisory{
				{CVE: "CVE-1234", Title: "Vulnerability 1"},
			},
			expected: []composer.Advisory{
				{CVE: "CVE-5678", Title: "Vulnerability 2"},
			},
		},
		{
			name: "All advisories fixed",
			before: []composer.Advisory{
				{CVE: "CVE-1234", Title: "Vulnerability 1"},
				{CVE: "CVE-5678", Title: "Vulnerability 2"},
			},
			after: []composer.Advisory{},
			expected: []composer.Advisory{
				{CVE: "CVE-1234", Title: "Vulnerability 1"},
				{CVE: "CVE-5678", Title: "Vulnerability 2"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ws := &ComposerAudit{
				beforeAudit: composer.Audit{Advisories: tt.before},
				afterAudit:  composer.Audit{Advisories: tt.after},
			}
			actual := ws.GetFixedAdvisories()
			assert.Equal(t, tt.expected, actual)
		})
	}
}
