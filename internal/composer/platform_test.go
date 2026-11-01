package composer

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/drupdater/drupdater/internal/agent"
)

func TestCheckPlatformReqsListsUnmetRequirementsButNotExtensions(t *testing.T) {
	service, sent := fakeAgentCLI(t, agentFixture(t, "platform-check/response.json"), 0)

	out, err := service.CheckPlatformReqs(t.Context(), t.TempDir())

	require.EqualError(t, err, "unmet platform requirements")
	assert.Equal(t, "php: fixture/site requires >=8.3, found 8.1.0", out)
	request, args := sent()
	assert.JSONEq(t, agentFixture(t, "platform-check/request.json"), request)
	assert.Equal(t, []string{"composer", "drupdater:platform-check", "--no-interaction", "--no-ansi"}, args)
}

func TestCheckPlatformReqsPassesWhenOnlyExtensionsAreUnmet(t *testing.T) {
	service, _ := fakeAgentCLI(t, `{"protocol":1,"command":"drupdater:platform-check","ok":true,"result":{"unmet":[`+
		`{"name":"ext-gd","version":null,"constraint":"*","required_by":"drupal/core"}]}}`, 0)

	out, err := service.CheckPlatformReqs(t.Context(), t.TempDir())

	require.NoError(t, err)
	assert.Empty(t, out)
}

func TestCheckPlatformReqsNamesAMissingPlatformPackage(t *testing.T) {
	service, _ := fakeAgentCLI(t, `{"protocol":1,"command":"drupdater:platform-check","ok":true,"result":{"unmet":[`+
		`{"name":"php-64bit","version":null,"constraint":"*","required_by":"drupal/core"},`+
		`{"name":"php","version":"8.1.0","constraint":">=8.3","required_by":"drupal/core"}]}}`, 0)

	out, err := service.CheckPlatformReqs(t.Context(), t.TempDir())

	require.Error(t, err)
	assert.Equal(t, "php-64bit: drupal/core requires *, found nothing\nphp: drupal/core requires >=8.3, found 8.1.0", out)
}

func TestCheckPlatformReqsReportsTheAgentsFailureAsTheDetail(t *testing.T) {
	service, _ := fakeAgentCLI(t, `{"protocol":1,"command":"drupdater:platform-check","ok":false,`+
		`"error":{"kind":"failed","reason":"the project has no composer.lock","message":""}}`, 1)

	out, err := service.CheckPlatformReqs(t.Context(), t.TempDir())

	var agentErr *agent.Error
	require.ErrorAs(t, err, &agentErr)
	assert.Equal(t, agent.Failed, agentErr.Kind)
	assert.Equal(t, "drupdater:platform-check: failed (the project has no composer.lock)", out)
}
