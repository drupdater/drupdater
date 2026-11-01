package composer

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/drupdater/drupdater/internal/agent"
)

func TestOpenAllowPluginsSendsTheRequestFixtureAndDecodesItsReply(t *testing.T) {
	service, sent := fakeAgentCLI(t, agentFixture(t, "allow-plugins/open-response.json"), 0)

	opened, err := service.OpenAllowPlugins(t.Context(), t.TempDir())

	require.NoError(t, err)
	request, args := sent()
	assert.JSONEq(t, agentFixture(t, "allow-plugins/open-request.json"), request)
	assert.Equal(t, []string{"composer", "drupdater:allow-plugins", "--no-interaction", "--no-ansi"}, args)
	assert.True(t, opened.Opened)
	assert.JSONEq(t, `{"cweagans/composer-patches": true, "drupal/*": false}`, string(opened.Original))
}

func TestCloseAllowPluginsSendsWhatOpeningReturnedAndDecodesTheDenials(t *testing.T) {
	service, sent := fakeAgentCLI(t, agentFixture(t, "allow-plugins/close-response.json"), 0)
	opened := PluginsOpened{Original: json.RawMessage(`{"cweagans/composer-patches": true, "drupal/*": false}`), Opened: true}

	denied, err := service.CloseAllowPlugins(t.Context(), t.TempDir(), opened)

	require.NoError(t, err)
	request, _ := sent()
	assert.JSONEq(t, agentFixture(t, "allow-plugins/close-request.json"), request)
	assert.Equal(t, []string{"dealerdirect/phpcodesniffer-composer-installer"}, denied)
}

func TestCloseAllowPluginsSendsNullForASettingNeverRecorded(t *testing.T) {
	service, sent := fakeAgentCLI(t, `{"protocol":1,"command":"drupdater:allow-plugins","ok":true,"result":{"denied":[]}}`, 0)

	_, err := service.CloseAllowPlugins(t.Context(), t.TempDir(), PluginsOpened{})

	require.NoError(t, err)
	request, _ := sent()
	assert.JSONEq(t, `{"protocol":1,"request":{"step":"close","original":null,"opened":false}}`, request)
}

func TestAllowPluginsReportsTheAgentsFailure(t *testing.T) {
	reply := `{"protocol":1,"command":"drupdater:allow-plugins","ok":false,` +
		`"error":{"kind":"internal","reason":"the agent failed","message":"composer.json is not writable"}}`

	service, _ := fakeAgentCLI(t, reply, 1)
	_, err := service.OpenAllowPlugins(t.Context(), t.TempDir())
	var agentErr *agent.Error
	require.ErrorAs(t, err, &agentErr)
	require.ErrorContains(t, err, "failed to allow plugins for the update: drupdater:allow-plugins: internal")

	service, _ = fakeAgentCLI(t, reply, 1)
	_, err = service.CloseAllowPlugins(t.Context(), t.TempDir(), PluginsOpened{})
	require.ErrorAs(t, err, &agentErr)
	require.ErrorContains(t, err, "failed to close allow-plugins after the update: drupdater:allow-plugins: internal")
}
