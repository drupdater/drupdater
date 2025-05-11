package cmd

import (
	"testing"

	"github.com/drupdater/drupdater/internal"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"
)

func TestNewLogger(t *testing.T) {
	t.Run("verbose mode", func(t *testing.T) {
		// Setup config with verbose mode enabled
		config := internal.Config{
			Verbose: true,
		}

		// Create logger
		logger := NewLogger(config)

		// Assert logger is in debug mode
		assert.NotNil(t, logger)
		assert.True(t, logger.Core().Enabled(zapcore.DebugLevel))
	})

	t.Run("non-verbose mode", func(t *testing.T) {
		// Setup config with verbose mode disabled
		config := internal.Config{
			Verbose: false,
		}

		// Create logger
		logger := NewLogger(config)

		// Assert logger is not in debug mode but is in info mode
		assert.NotNil(t, logger)
		assert.False(t, logger.Core().Enabled(zapcore.DebugLevel))
		assert.True(t, logger.Core().Enabled(zapcore.InfoLevel))
	})
}

func TestNewCache(t *testing.T) {
	// Create cache
	cache := NewCache()

	// Verify cache is initialized
	assert.NotNil(t, cache)

	// Test basic cache operations
	cache.Set("test_key", "test_value")
	value, found := cache.Get("test_key")

	assert.True(t, found)
	assert.Equal(t, "test_value", value)
}

func TestCreateAddons(t *testing.T) {
	// Setup minimal test dependencies
	logger := zaptest.NewLogger(t)

	// Test with default config
	t.Run("default config", func(t *testing.T) {
		config := internal.Config{
			SkipCBF:    true,  // Skip code beautifier
			SkipRector: true,  // Skip rector
			Security:   false, // No security-only updates
		}

		addons := createAddons(
			logger,
			config,
			nil, // mock would be better here
			nil, // mock would be better here
			nil, // mock would be better here
			nil, // mock would be better here
		)

		// Should have the basic addons (6 addons)
		assert.Len(t, addons, 6)
	})

	// Test with security mode enabled
	t.Run("security mode", func(t *testing.T) {
		config := internal.Config{
			SkipCBF:    true,
			SkipRector: true,
			Security:   true, // Enable security-only updates
		}

		addons := createAddons(
			logger,
			config,
			nil, // mock would be better here
			nil, // mock would be better here
			nil, // mock would be better here
			nil, // mock would be better here
		)

		// Should have the basic addons + security addon
		assert.Len(t, addons, 7)
	})
}
