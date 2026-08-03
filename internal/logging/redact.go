// Package logging wraps a zapcore.Core to redact known secret values before they reach any sink.
// Subprocesses echo credentials back in their own output; wrapping the logger covers every call site.
package logging

import (
	"errors"
	"maps"
	"net/url"
	"slices"
	"strings"
	"sync"

	"go.uber.org/zap/zapcore"
)

// placeholder replaces every registered secret value wherever it appears in log output.
const placeholder = "***"

// Redactor holds the exact secret values to hide. Value-based, not pattern-based: the real values
// are known at startup, and "looks like a token" both misses formats and mangles innocent output.
type Redactor struct {
	mu       sync.RWMutex
	secrets  map[string]struct{}
	replacer *strings.Replacer
}

// NewRedactor returns an empty Redactor. Register values as soon as they are known.
func NewRedactor() *Redactor {
	return &Redactor{secrets: map[string]struct{}{}}
}

// Register adds values to redact from all later log output. Empty strings are ignored: redacting
// "" would replace every character in every line. Both URL escapings go in too — query and path
// escaping differ on a space, so one form alone leaves the value readable.
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
		add(url.PathEscape(v))
	}
	if changed {
		r.replacer = nil // rebuilt lazily by redact on next use
	}
}

// Redact is the exported counterpart of redact, for output that bypasses the logger: a preflight
// check's results, or the run report.
func (r *Redactor) Redact(s string) string {
	return r.redact(s)
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

	values := slices.Collect(maps.Keys(r.secrets))
	// Longest first, so a secret that is a substring of another is not partially consumed.
	slices.SortFunc(values, func(a, b string) int { return len(b) - len(a) })

	pairs := make([]string, 0, len(values)*2)
	for _, v := range values {
		pairs = append(pairs, v, placeholder)
	}
	r.replacer = strings.NewReplacer(pairs...)
	return r.replacer
}

// core rewrites the message and any string/error field before handing the entry on.
type core struct {
	zapcore.Core
	redactor *Redactor
}

// WrapCore returns a constructor for zap.WrapCore, so every entry is filtered, Debug included.
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

// redactFields returns a copy of fields with string and error values redacted.
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
			// Numbers and durations pass through: no call site puts subprocess output there.
		}
		out[i] = f
	}
	return out
}
