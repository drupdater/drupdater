package logging

import (
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// A missed redaction is a credential in a log. The property test draws secrets from the alphabet
// real tokens use; fuzzing drops that restriction, so the promise is checked against bytes no
// generator would think to produce — invalid UTF-8, lone percent signs, embedded NULs.
func FuzzRedact(f *testing.F) {
	for _, seed := range [][3]string{
		{"glpat-abc123", "ghp_def456", "cloning https://oauth2:glpat-abc123@gitlab.com/o/r.git"},
		{"tok en", "tok", "tok en and tok"},
		{"secret", "secret", "secret"},
		{"%2F", "/", "a%2Fb"},
		{"pässwörd", "ß", "pässwörd and ß"},
		{"", "x", "x"},
		{"a", "ba", "ab ba"},
	} {
		f.Add(seed[0], seed[1], seed[2])
	}

	f.Fuzz(func(t *testing.T, first, second, text string) {
		// The placeholder is asterisks, so a secret containing one can be reassembled out of a
		// replacement — a leak by construction rather than a defect. Same exclusion the property
		// test makes, stated on the character that causes it.
		if strings.Contains(first, "*") || strings.Contains(second, "*") {
			t.Skip("a secret containing the placeholder character cannot be hidden by it")
		}

		redactor := NewRedactor()
		redactor.Register(first, second)
		out := redactor.Redact(text)

		for _, secret := range []string{first, second} {
			// Register ignores "": redacting it would replace every character of every line.
			if secret == "" {
				continue
			}
			assert.NotContains(t, out, secret)
			// Which escaping a subprocess emits depends on where in a URL the token landed, and
			// either form is still a usable credential.
			assert.NotContains(t, out, url.QueryEscape(secret))
			assert.NotContains(t, out, url.PathEscape(secret))
		}

		// Log lines pass the redactor once per sink; a second pass must not change what the
		// first one produced.
		assert.Equal(t, out, redactor.Redact(out))
	})
}
