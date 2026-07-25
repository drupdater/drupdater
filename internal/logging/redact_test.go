package logging

import (
	"bytes"
	"fmt"
	"net/url"
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

func TestRedactorRedactsStringFields(t *testing.T) {
	const token = "field-secret"

	var buf bytes.Buffer
	redactor := NewRedactor()
	redactor.Register(token)
	logger := newTestLogger(&buf, redactor)

	logger.Info("command output", zap.String("output", "auth failed with token "+token))

	out := buf.String()
	assert.NotContains(t, out, token)
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
	assert.NotContains(t, out, "secret-extended")
	assert.NotContains(t, out, "secret")
}

func TestCoreSyncDelegates(t *testing.T) {
	var buf bytes.Buffer
	redactor := NewRedactor()
	logger := newTestLogger(&buf, redactor)

	require.NoError(t, logger.Sync())
}
