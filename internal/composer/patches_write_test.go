package composer

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/drupdater/drupdater/internal/agent"
)

func TestWritePatchesSendsThePatchesAsGiven(t *testing.T) {
	service, sent := fakeAgentCLI(t, agentFixture(t, "write-patches/response.json"), 0)
	patches := json.RawMessage(`{"drupal/token":{` +
		`"Issue #3123456: [Token fix](https://www.drupal.org/i/3123456)":"patches/token/3123456-local.patch",` +
		`"Remote fix, rerolled":"https://www.drupal.org/files/issues/3200000-9.patch"},"drupal/empty":{}}`)

	err := service.WritePatches(t.Context(), t.TempDir(), patches)

	require.NoError(t, err)
	request, args := sent()
	assert.JSONEq(t, agentFixture(t, "write-patches/request.json"), request)
	assert.Contains(t, request, `"drupal/empty":{}`, "an empty patch list reaches the agent as an object")
	assert.Equal(t, []string{"composer", "drupdater:write-patches", "--no-interaction", "--no-ansi"}, args)
}

func TestWritePatchesReportsTheAgentsFailure(t *testing.T) {
	service, _ := fakeAgentCLI(t, `{"protocol":1,"command":"drupdater:write-patches","ok":false,`+
		`"error":{"kind":"failed","reason":"the project has no composer.lock","message":""}}`, 1)

	err := service.WritePatches(t.Context(), t.TempDir(), json.RawMessage(`{}`))

	var agentErr *agent.Error
	require.ErrorAs(t, err, &agentErr)
	assert.Equal(t, agent.Failed, agentErr.Kind)
	assert.ErrorContains(t, err, "failed to write the patches: drupdater:write-patches: failed (the project has no composer.lock)")
}
