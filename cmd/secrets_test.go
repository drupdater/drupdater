package cmd

import (
	"testing"

	"github.com/drupdater/drupdater/internal/config"
	"github.com/drupdater/drupdater/internal/logging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestResolveToken(t *testing.T) {
	// The zero-value config (no --clone, no --dry-run) requires a token, matching a normal run.
	normalRun := config.Config{}

	t.Run("the positional argument wins", func(t *testing.T) {
		t.Setenv("DRUPDATER_TOKEN", "from-env")

		token, err := resolveToken([]string{"from-arg"}, normalRun)
		require.NoError(t, err)
		assert.Equal(t, "from-arg", token)
	})

	t.Run("falls back to DRUPDATER_TOKEN", func(t *testing.T) {
		t.Setenv("DRUPDATER_TOKEN", "from-env")

		token, err := resolveToken(nil, normalRun)
		require.NoError(t, err)
		assert.Equal(t, "from-env", token)
	})

	t.Run("an empty argument falls back to the environment", func(t *testing.T) {
		t.Setenv("DRUPDATER_TOKEN", "from-env")

		token, err := resolveToken([]string{""}, normalRun)
		require.NoError(t, err)
		assert.Equal(t, "from-env", token)
	})

	t.Run("errors when neither is set", func(t *testing.T) {
		t.Setenv("DRUPDATER_TOKEN", "")

		_, err := resolveToken(nil, normalRun)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "DRUPDATER_TOKEN")
	})

	t.Run("a checkout-mode dry-run needs no token", func(t *testing.T) {
		t.Setenv("DRUPDATER_TOKEN", "")

		token, err := resolveToken(nil, config.Config{DryRun: true})
		require.NoError(t, err)
		assert.Empty(t, token)
	})

	t.Run("a checkout-mode dry-run still honors a given token", func(t *testing.T) {
		t.Setenv("DRUPDATER_TOKEN", "from-env")

		token, err := resolveToken(nil, config.Config{DryRun: true})
		require.NoError(t, err)
		assert.Equal(t, "from-env", token)
	})

	t.Run("--clone still requires a token even with --dry-run", func(t *testing.T) {
		t.Setenv("DRUPDATER_TOKEN", "")

		_, err := resolveToken(nil, config.Config{Clone: true, DryRun: true})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "DRUPDATER_TOKEN")
	})
}

func TestTokenRequired(t *testing.T) {
	t.Run("a normal checkout run requires a token", func(t *testing.T) {
		assert.True(t, tokenRequired(config.Config{}))
	})

	t.Run("--clone requires a token even with --dry-run", func(t *testing.T) {
		assert.True(t, tokenRequired(config.Config{Clone: true, DryRun: true}))
	})

	t.Run("checkout mode with --dry-run needs no token", func(t *testing.T) {
		assert.False(t, tokenRequired(config.Config{DryRun: true}))
	})
}

func TestRegisterEnvSecrets(t *testing.T) {
	// One wiring for both a real run and "drupdater check": same subprocesses, same secrets.
	t.Setenv("DRUPALCODE_ACCESS_TOKEN", "drupalcode-secret")
	t.Setenv("COMPOSER_AUTH", `{"bearer":{"example.com":"bearer-secret"}}`)

	redactor := logging.NewRedactor()
	registerEnvSecrets(redactor)

	got := redactor.Redact("leaked drupalcode-secret and bearer-secret")
	assert.NotContains(t, got, "drupalcode-secret")
	assert.NotContains(t, got, "bearer-secret")
}

func TestRegisterComposerAuth(t *testing.T) {
	// The individual credentials, not just the raw blob: Composer echoes the leaf, never the
	// JSON verbatim.
	redact := func(t *testing.T, redactor *logging.Redactor, msg string) string {
		t.Helper()
		core, logs := observer.New(zap.DebugLevel)
		logger := zap.New(logging.WrapCore(redactor)(core))
		logger.Info(msg)
		return logs.All()[0].Message
	}

	t.Run("registers individual leaf values from valid JSON", func(t *testing.T) {
		redactor := logging.NewRedactor()
		registerComposerAuth(redactor, `{"http-basic":{"repo.packagist.com":{"username":"du","password":"s3cr3t-pass"}}}`)

		got := redact(t, redactor, "fetch failed: https://du:s3cr3t-pass@repo.packagist.com/p2/foo.json: 403")
		assert.NotContains(t, got, "s3cr3t-pass")
	})

	t.Run("falls back to redacting the raw value when it is not valid JSON", func(t *testing.T) {
		redactor := logging.NewRedactor()
		registerComposerAuth(redactor, "not-json-token")

		got := redact(t, redactor, "leaked not-json-token here")
		assert.NotContains(t, got, "not-json-token")
	})

	t.Run("empty value is a no-op", func(t *testing.T) {
		redactor := logging.NewRedactor()
		registerComposerAuth(redactor, "")

		got := redact(t, redactor, "hello world")
		assert.Equal(t, "hello world", got)
	})
}

func TestComposerAuthSecretLeaves(t *testing.T) {
	leaves := composerAuthSecretLeaves(map[string]any{
		"a": "1",
		"b": map[string]any{"c": "2", "d": []any{"3", "4"}},
		"e": 5.0,
	}, "")
	assert.ElementsMatch(t, []string{"1", "2", "3", "4"}, leaves)
}

func TestComposerAuthSecretLeavesSkipsUsername(t *testing.T) {
	// http-basic sets username to the literal word "token", which must stay out of the secret
	// set or every unrelated "token" in the log is redacted too.
	leaves := composerAuthSecretLeaves(map[string]any{
		"http-basic": map[string]any{
			"repo.packagist.com": map[string]any{
				"username": "token",
				"password": "s3cr3t-pass",
			},
		},
	}, "")
	assert.Equal(t, []string{"s3cr3t-pass"}, leaves)
}
