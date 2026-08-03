package logging

import (
	"bytes"
	"fmt"
	"net/url"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// newTestLogger builds a zap.Logger writing JSON lines into buf, with its core wrapped by r,
// mirroring how NewLogger wires the redactor into the real logger.
func newTestLogger(buf *bytes.Buffer, r *Redactor) *zap.Logger {
	encoder := zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig())
	base := zapcore.NewCore(encoder, zapcore.AddSync(buf), zapcore.DebugLevel)
	return zap.New(WrapCore(r)(base))
}

func TestRedactorRedactsSecretsFromLogOutput(t *testing.T) {
	const token = "gh_super_secret_token"

	var buf bytes.Buffer
	redactor := NewRedactor()
	redactor.Register(token)
	logger := newTestLogger(&buf, redactor)

	logger.Debug("composer clone failed: " + fmt.Sprintf("https://%s@example.com/repo.git: 403", token))

	out := buf.String()
	assert.NotContains(t, out, token)
	assert.Contains(t, out, "***")
}

func TestRedactorRedactsPercentEncodedForm(t *testing.T) {
	const token = "a b+c/d"

	var buf bytes.Buffer
	redactor := NewRedactor()
	redactor.Register(token)
	logger := newTestLogger(&buf, redactor)

	encoded := url.QueryEscape(token)
	require.NotEqual(t, token, encoded)

	logger.Info("fetch failed for https://example.com/repo.git?token=" + encoded)

	out := buf.String()
	assert.NotContains(t, out, token)
	assert.NotContains(t, out, encoded)
}

// The same token is "a+b" after query escaping and "a%20b" after path escaping. Registering
// only the query form left the other — still a usable credential — readable in the log.
func TestRedactorRedactsPathEncodedForm(t *testing.T) {
	const token = "a b c"

	var buf bytes.Buffer
	redactor := NewRedactor()
	redactor.Register(token)
	logger := newTestLogger(&buf, redactor)

	pathEncoded := url.PathEscape(token)
	require.NotEqual(t, url.QueryEscape(token), pathEncoded)

	logger.Info("clone failed for https://example.com/" + pathEncoded + "/repo.git")

	out := buf.String()
	assert.NotContains(t, out, token)
	assert.NotContains(t, out, pathEncoded)
	assert.Contains(t, out, "https://example.com/***/repo.git")
}

func TestRedactorRedactsStringFields(t *testing.T) {
	const token = "field-secret"

	var buf bytes.Buffer
	redactor := NewRedactor()
	redactor.Register(token)
	logger := newTestLogger(&buf, redactor)

	logger.Info("command output", zap.String("output", "auth failed with token "+token),
		zap.String("step", "composer update"), zap.Int("attempt", 2))

	out := buf.String()
	assert.NotContains(t, out, token)
	// Assert what the field became, not just that the secret is gone: dropping the fields
	// entirely would also satisfy NotContains.
	assert.Contains(t, out, `"output":"auth failed with token ***"`)
	// Fields with nothing to redact must survive untouched, including non-string kinds.
	assert.Contains(t, out, `"step":"composer update"`)
	assert.Contains(t, out, `"attempt":2`)
}

func TestRedactorRedactsErrorFields(t *testing.T) {
	const token = "err-secret"

	var buf bytes.Buffer
	redactor := NewRedactor()
	redactor.Register(token)
	logger := newTestLogger(&buf, redactor)

	err := fmt.Errorf("composer update failed: %w", fmt.Errorf("401 for https://%s@example.com", token))
	logger.Error("update failed", zap.Error(err))

	out := buf.String()
	assert.NotContains(t, out, token)
	// The error field survives with only the secret replaced -- the surrounding diagnostic
	// text is the reason the entry is being logged at all.
	assert.Contains(t, out, `"error":"composer update failed: 401 for https://***@example.com"`)
}

func TestRedactorRedactsFieldsAddedWithWith(t *testing.T) {
	const token = "with-secret"

	var buf bytes.Buffer
	redactor := NewRedactor()
	redactor.Register(token)
	logger := newTestLogger(&buf, redactor).With(zap.String("output", "leaked "+token))

	logger.Info("done")

	out := buf.String()
	assert.NotContains(t, out, token)
	assert.Contains(t, out, `"output":"leaked ***"`)
}

func TestRedactorIgnoresEmptyValues(t *testing.T) {
	var buf bytes.Buffer
	redactor := NewRedactor()
	redactor.Register("")
	logger := newTestLogger(&buf, redactor)

	logger.Info("hello world")

	out := buf.String()
	assert.NotContains(t, out, "***")
	assert.Contains(t, out, "hello world")
}

func TestRedactorLongestSecretWinsOverSubstring(t *testing.T) {
	var buf bytes.Buffer
	redactor := NewRedactor()
	redactor.Register("secret")
	redactor.Register("secret-extended")
	logger := newTestLogger(&buf, redactor)

	logger.Info("value: secret-extended")

	out := buf.String()
	// The whole result: shortest-first yields "***-extended", which an absence check passes.
	assert.Contains(t, out, `"msg":"value: ***"`)
	assert.NotContains(t, out, "-extended")
}

func TestRedactorRebuildsReplacerAfterNewSecret(t *testing.T) {
	redactor := NewRedactor()
	redactor.Register("first")
	// Force the replacer to be built, so the next Register has a cached one to invalidate.
	require.Equal(t, "***", redactor.Redact("first"))

	redactor.Register("second")

	// A secret registered after the first log line must still be redacted -- credentials are
	// discovered mid-run (Composer auth, a resolved clone URL), not all at startup.
	assert.Equal(t, "***", redactor.Redact("second"))
	assert.Equal(t, "***", redactor.Redact("first"))
}

func TestRedactorKeepsReplacerWhenNothingChanged(t *testing.T) {
	redactor := NewRedactor()
	redactor.Register("token")
	built := redactor.currentReplacer()
	require.NotNil(t, built)

	redactor.Register("token") // already known
	redactor.Register("")      // ignored

	// Register is called repeatedly as credentials resolve, so rebuilding on every call would
	// grow redaction's cost with the call count rather than the number of secrets.
	assert.Same(t, built, redactor.currentReplacer())
}

func TestRedactorRegistersRawAndEncodedForms(t *testing.T) {
	// Characters QueryEscape rewrites, so the two forms are genuinely different strings.
	const secret = "p@ss word/1"
	encoded := url.QueryEscape(secret)
	require.NotEqual(t, secret, encoded)

	redactor := NewRedactor()
	redactor.Register(secret)

	assert.Equal(t, "***", redactor.Redact(secret))
	assert.Equal(t, "***", redactor.Redact(encoded))
}

func TestRedactorRegisterIsSafeForConcurrentUse(t *testing.T) {
	redactor := NewRedactor()

	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			redactor.Register(fmt.Sprintf("secret-%d", i))
		}()
	}
	wg.Wait()

	for i := range 8 {
		assert.Equal(t, "***", redactor.Redact(fmt.Sprintf("secret-%d", i)))
	}
}

func TestCoreSkipsEntriesBelowLevel(t *testing.T) {
	var buf bytes.Buffer
	redactor := NewRedactor()

	encoder := zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig())
	base := zapcore.NewCore(encoder, zapcore.AddSync(&buf), zapcore.InfoLevel)
	logger := zap.New(WrapCore(redactor)(base))

	logger.Debug("below the level")
	logger.Info("at the level")

	out := buf.String()
	assert.NotContains(t, out, "below the level")
	assert.Contains(t, out, "at the level")
}

func TestCoreSyncDelegates(t *testing.T) {
	var buf bytes.Buffer
	redactor := NewRedactor()
	logger := newTestLogger(&buf, redactor)

	require.NoError(t, logger.Sync())
}
