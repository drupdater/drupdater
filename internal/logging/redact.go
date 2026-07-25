// Package logging provides a zapcore.Core wrapper that redacts known secret values (access
// tokens, Composer auth credentials) from log output before it reaches any sink. Subprocesses
// drupdater shells out to (Composer, Drush, git) can echo those values in their own output, most
// often in a URL when an authenticated fetch fails; wrapping the logger means every call site is
// covered without having to remember to sanitize each one individually.
package logging

import (
	"errors"
	"net/url"
	"sort"
	"strings"
	"sync"

	"go.uber.org/zap/zapcore"
)

// placeholder replaces every registered secret value wherever it appears in log output.
const placeholder = "***"

// Redactor holds the exact secret values that must never appear in log output. Redaction is
// value-based, not pattern-based: matching "things that look like a token" both misses unusual
// formats and mangles innocent output, whereas the exact secret values are known at startup.
// Safe for concurrent use.
type Redactor struct {
	mu       sync.RWMutex
	secrets  map[string]struct{}
	replacer *strings.Replacer
}

// NewRedactor returns an empty Redactor. Register secret values as soon as they are known so
// nothing is logged unredacted in between.
func NewRedactor() *Redactor {
	return &Redactor{secrets: map[string]struct{}{}}
}

// Register adds values that must be redacted from all subsequent log output. Empty strings are
// ignored, since redacting "" would replace every character in every log line. Each value's
// URL-percent-encoded form is registered alongside it: a token embedded in a URL (e.g. a
// Composer repository URL) may appear encoded rather than literal.
func (r *Redactor) Register(values ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	changed := false
	add := func(v string) {
		if v == "" {
			return
		}
		if _, ok := r.secrets[v]; !ok {
			r.secrets[v] = struct{}{}
			changed = true
		}
	}
	for _, v := range values {
		add(v)
		add(url.QueryEscape(v))
	}
	if changed {
		r.replacer = nil // rebuilt lazily by redact on next use
	}
}

// redact replaces every registered secret value in s with the placeholder.
func (r *Redactor) redact(s string) string {
	replacer := r.currentReplacer()
	if replacer == nil {
		return s
	}
	return replacer.Replace(s)
}

func (r *Redactor) currentReplacer() *strings.Replacer {
	r.mu.RLock()
	replacer := r.replacer
	empty := len(r.secrets) == 0
	r.mu.RUnlock()
	if replacer != nil || empty {
		return replacer
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.replacer != nil {
		return r.replacer
	}
	if len(r.secrets) == 0 {
		return nil
	}

	values := make([]string, 0, len(r.secrets))
	for v := range r.secrets {
		values = append(values, v)
	}
	// Longest first, so a secret that happens to be a substring of another registered value
	// is still matched in full rather than being partially consumed by the shorter one.
	sort.Slice(values, func(i, j int) bool { return len(values[i]) > len(values[j]) })

	pairs := make([]string, 0, len(values)*2)
	for _, v := range values {
		pairs = append(pairs, v, placeholder)
	}
	r.replacer = strings.NewReplacer(pairs...)
	return r.replacer
}

// core wraps another zapcore.Core, rewriting the log message and any string/error field through
// the Redactor before handing the entry on. It never logs the secret set itself.
type core struct {
	zapcore.Core
	redactor *Redactor
}

// WrapCore returns a zap.Option-compatible constructor that wraps a logger's core with
// redaction using r. Pass it to zap.WrapCore (or Config.Build) so every entry the logger emits,
// at every level including Debug, is filtered before it reaches its sink.
func WrapCore(r *Redactor) func(zapcore.Core) zapcore.Core {
	return func(c zapcore.Core) zapcore.Core {
		return &core{Core: c, redactor: r}
	}
}

func (c *core) With(fields []zapcore.Field) zapcore.Core {
	return &core{Core: c.Core.With(c.redactFields(fields)), redactor: c.redactor}
}

func (c *core) Check(entry zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if c.Enabled(entry.Level) {
		return ce.AddCore(entry, c)
	}
	return ce
}

func (c *core) Write(entry zapcore.Entry, fields []zapcore.Field) error {
	entry.Message = c.redactor.redact(entry.Message)
	return c.Core.Write(entry, c.redactFields(fields))
}

// redactFields returns a copy of fields with string and error values passed through the
// redactor.
func (c *core) redactFields(fields []zapcore.Field) []zapcore.Field {
	out := make([]zapcore.Field, len(fields))
	for i, f := range fields {
		switch f.Type {
		case zapcore.StringType:
			f.String = c.redactor.redact(f.String)
		case zapcore.ErrorType:
			if err, ok := f.Interface.(error); ok {
				f.Interface = errors.New(c.redactor.redact(err.Error()))
			}
		default:
			// Other kinds (numbers, durations, zap.Any/zap.Strings, ...) are passed through
			// unchanged: no call site in this codebase puts subprocess output into those.
		}
		out[i] = f
	}
	return out
}
