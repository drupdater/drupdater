package composer

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/drupdater/drupdater/internal/agent"
)

func TestUpdateSendsTheRequestFixtureAndDecodesItsChanges(t *testing.T) {
	service, sent := fakeAgentCLI(t, agentFixture(t, "resolve/response.json"), 0)

	changes, err := service.Update(t.Context(), t.TempDir(), nil, nil, false, true)

	require.NoError(t, err)
	request, args := sent()
	assert.JSONEq(t, agentFixture(t, "resolve/request.json"), request)
	assert.Equal(t, []string{"composer", "drupdater:resolve", "--no-interaction", "--no-ansi"}, args)
	assert.Equal(t, []PackageChange{
		{Action: "Upgrade", Package: "fixture/a", From: "1.0.0", To: "1.1.0"},
		{Action: "Upgrade", Package: "fixture/b", From: "2.0.0", To: "2.1.0"},
	}, changes)
}

func TestUpdatePassesThePlanOn(t *testing.T) {
	service, sent := fakeAgentCLI(t, `{"protocol":1,"command":"drupdater:resolve","ok":true,"result":{"changes":[]}}`, 0)

	changes, err := service.Update(t.Context(), t.TempDir(), []string{"drupal/core"},
		[]string{"drupal/token:1.13.0", "symfony/polyfill-ctype:1.x-dev 0424dff"}, true, false)

	require.NoError(t, err)
	assert.Nil(t, changes, "nothing changed")
	request, _ := sent()
	assert.JSONEq(t, `{"protocol":1,"request":{"packages":["drupal/core"],"keep":[`+
		`{"package":"drupal/token","version":"1.13.0"},{"package":"symfony/polyfill-ctype","version":"1.x-dev 0424dff"}],`+
		`"minimal_changes":true,"write":true}}`, request)
}

func TestUpdateReportsAFailedResolutionWithComposersExplanation(t *testing.T) {
	service, _ := fakeAgentCLI(t, `{"protocol":1,"command":"drupdater:resolve","ok":false,`+
		`"error":{"kind":"failed","reason":"composer update failed","message":"exit code 2"}}`, 1)

	_, err := service.Update(t.Context(), t.TempDir(), nil, nil, false, true)

	var agentErr *agent.Error
	require.ErrorAs(t, err, &agentErr)
	assert.Equal(t, agent.Failed, agentErr.Kind)
	assert.ErrorContains(t, err, "failed to update dependencies: drupdater:resolve: failed (composer update failed): exit code 2")
}
