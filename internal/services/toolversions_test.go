package services

import (
	"errors"
	"testing"

	"github.com/drupdater/drupdater/internal/report"
	"github.com/drupdater/drupdater/pkg/composer"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestLookupToolVersions(t *testing.T) {
	core, logs := observer.New(zapcore.InfoLevel)
	composerMock := NewMockComposerVersionReporter(t)
	composerMock.EXPECT().Version(anyCtx).Return(composer.Versions{Composer: "2.10.2", PHP: "8.3.14"}, nil)

	versions := LookupToolVersions(t.Context(), zap.New(core), composerMock)

	assert.Equal(t, report.ToolVersions{ComposerVersion: "2.10.2", PHPVersion: "8.3.14"}, versions)

	// Logged as well as recorded, so a run without --report says which tools produced it too.
	entries := logs.FilterMessage("tool versions").All()
	assert.Len(t, entries, 1)
	assert.Equal(t, map[string]any{"composer": "2.10.2", "php": "8.3.14"}, entries[0].ContextMap())
}

func TestLookupToolVersionsWarnsAndYieldsNothingOnFailure(t *testing.T) {
	core, logs := observer.New(zapcore.WarnLevel)
	composerMock := NewMockComposerVersionReporter(t)
	composerMock.EXPECT().Version(anyCtx).Return(composer.Versions{}, errors.New("exec: composer not found"))

	versions := LookupToolVersions(t.Context(), zap.New(core), composerMock)

	assert.Equal(t, report.ToolVersions{}, versions)
	assert.Equal(t, 1, logs.FilterMessage("could not determine the composer version").Len())
}
