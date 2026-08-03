package logging

import (
	"bytes"
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"pgregory.net/rapid"
)

// A case nobody thought of means a credential in a log, so the redactor's promises are stated
// as properties over generated input.

// secretGen draws from the alphabet real tokens use, plus the separators found around them in
// URLs, so the values exercise the URL-encoded registration.
//
// '*' is excluded: the placeholder is "***", so an all-asterisk secret is a substring of the
// thing that replaces it and would reappear by construction.
func secretGen() *rapid.Generator[string] {
	return rapid.StringMatching(`[a-zA-Z0-9_.:/+= @-]{1,20}`)
}

// secretsGen generates a small set of distinct secrets, the way a run registers a token, a
// Composer password and a git credential.
func secretsGen() *rapid.Generator[[]string] {
	return rapid.SliceOfNDistinct(secretGen(), 1, 4, rapid.ID)
}

// logLineGen plants the secrets in the line. A wholly random string would almost never contain
// one, and the property would pass without ever replacing anything.
func logLineGen(secrets []string) *rapid.Generator[string] {
	parts := make([]*rapid.Generator[string], 0, len(secrets)+1)
	parts = append(parts, rapid.String())
	for _, secret := range secrets {
		parts = append(parts, rapid.Just(secret))
	}
	part := rapid.OneOf(parts...)

	return rapid.Custom(func(t *rapid.T) string {
		var line strings.Builder
		for range rapid.IntRange(0, 8).Draw(t, "parts") {
			line.WriteString(part.Draw(t, "part"))
		}
		return line.String()
	})
}

func TestPropertyRedactNeverLeaksASecret(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		secrets := secretsGen().Draw(t, "secrets")
		line := logLineGen(secrets).Draw(t, "line")

		redactor := NewRedactor()
		redactor.Register(secrets...)

		out := redactor.Redact(line)
		for _, secret := range secrets {
			assert.NotContains(t, out, secret)
		}
	})
}

func TestPropertyRedactIsIdempotent(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		secrets := secretsGen().Draw(t, "secrets")
		line := logLineGen(secrets).Draw(t, "line")

		redactor := NewRedactor()
		redactor.Register(secrets...)

		once := redactor.Redact(line)
		assert.Equal(t, once, redactor.Redact(once))
	})
}

func TestPropertyRedactCoversURLEncodedForms(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		secret := secretGen().Draw(t, "secret")

		redactor := NewRedactor()
		redactor.Register(secret)

		// Which escaping a subprocess emits depends on where in the URL the token landed, and
		// either form is still a usable credential — so both must be registered.
		assert.Equal(t, placeholder, redactor.Redact(secret))
		assert.Equal(t, placeholder, redactor.Redact(url.QueryEscape(secret)))
		assert.Equal(t, placeholder, redactor.Redact(url.PathEscape(secret)))
	})
}

func TestPropertyRedactLeavesSecretFreeTextAlone(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Disjoint alphabets, so "this filler holds no secret" is true by construction rather
		// than by luck — encoding introduces only '%' and hex digits, which bridge nothing.
		secrets := rapid.SliceOfNDistinct(rapid.StringMatching(`[a-z0-9]{1,12}`), 1, 4, rapid.ID).Draw(t, "secrets")
		text := rapid.StringMatching(`[ ,.!?()\[\]]{0,40}`).Draw(t, "text")

		redactor := NewRedactor()
		redactor.Register(secrets...)

		assert.Equal(t, text, redactor.Redact(text))
	})
}

func TestPropertyRegisterIsOrderIndependent(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		secrets := secretsGen().Draw(t, "secrets")
		shuffled := rapid.Permutation(secrets).Draw(t, "shuffled")
		line := logLineGen(secrets).Draw(t, "line")

		together := NewRedactor()
		together.Register(secrets...)

		separately := NewRedactor()
		for _, secret := range shuffled {
			separately.Register(secret)
		}

		// Registration order is not something a caller controls — cmd/root.go registers as the
		// values become known — so it must not change what a log line turns into.
		assert.Equal(t, together.Redact(line), separately.Redact(line))
	})
}

func TestPropertyRedactReplacesTheLongestSecret(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		outer := rapid.StringMatching(`[a-z0-9]{2,20}`).Draw(t, "outer")
		start := rapid.IntRange(0, len(outer)-1).Draw(t, "start")
		end := rapid.IntRange(start+1, len(outer)).Draw(t, "end")
		inner := outer[start:end]

		redactor := NewRedactor()
		redactor.Register(outer, inner)

		// A short secret that happens to be part of a longer one must not consume it piecemeal
		// and leave the rest of the longer value readable.
		assert.Equal(t, placeholder, redactor.Redact(outer))
	})
}

func TestPropertyWrappedLoggerNeverWritesASecret(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		secrets := secretsGen().Draw(t, "secrets")
		message := logLineGen(secrets).Draw(t, "message")
		field := logLineGen(secrets).Draw(t, "field")

		var buf bytes.Buffer
		redactor := NewRedactor()
		redactor.Register(secrets...)
		newTestLogger(&buf, redactor).Info(message, zap.String("detail", field))

		// On the decoded entry, not the raw line: a one-character secret like "8" occurs in
		// zap's own timestamp, which is not what the promise is about.
		var entry struct {
			Message string `json:"msg"`
			Detail  string `json:"detail"`
		}
		require.NoError(t, json.Unmarshal(buf.Bytes(), &entry))

		for _, secret := range secrets {
			assert.NotContains(t, entry.Message, secret)
			assert.NotContains(t, entry.Detail, secret)
		}
	})
}
