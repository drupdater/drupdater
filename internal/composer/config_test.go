package composer

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/drupdater/drupdater/internal/agent"
)

func TestConfigSendsTheRequestFixtureAndDecodesTheConfiguration(t *testing.T) {
	service, sent := fakeAgentCLI(t, agentFixture(t, "config/response.json"), 0)

	reply, err := service.Config(t.Context(), "/tmp/drupdater-config-fixture/.drupdater.yaml")

	require.NoError(t, err)
	body, args := sent()
	assert.JSONEq(t, agentFixture(t, "config/request.json"), body)
	assert.Contains(t, args, "drupdater:config")
	assert.True(t, reply.Found)
	assert.Equal(t, []string{"default", "second"}, reply.Sites)
	assert.Equal(t, int64(90*time.Minute), reply.TimeoutNS)
	assert.Equal(t, "code_beautifier", reply.RunTypes.Normal.Addons[len(reply.RunTypes.Normal.Addons)-1])
	assert.False(t, reply.RunTypes.Normal.AutoMerge)
	assert.True(t, reply.RunTypes.Security.AutoMerge)
}

func TestConfigSendsTheAgentAnAbsolutePath(t *testing.T) {
	service, sent := fakeAgentCLI(t, agentFixture(t, "config/response.json"), 0)
	t.Chdir(t.TempDir())
	cwd, err := filepath.Abs(".")
	require.NoError(t, err)

	_, err = service.Config(t.Context(), ".drupdater.yaml")

	require.NoError(t, err)
	body, _ := sent()
	var request struct {
		Request agent.ConfigRequest `json:"request"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &request))
	assert.Equal(t, filepath.Join(cwd, ".drupdater.yaml"), request.Request.Path)
}

func TestARefusedConfigurationIsTheReasonToFixAlone(t *testing.T) {
	service, _ := fakeAgentCLI(t, `{"protocol":6,"command":"drupdater:config","ok":false,`+
		`"error":{"kind":"failed","reason":"in /p/.drupdater.yaml: no sites configured: \"sites\" must list at least one Drupal site name","message":""}}`, 1)

	_, err := service.Config(t.Context(), "/p/.drupdater.yaml")

	var invalid *InvalidConfigError
	require.ErrorAs(t, err, &invalid)
	assert.EqualError(t, err, `in /p/.drupdater.yaml: no sites configured: "sites" must list at least one Drupal site name`)
}

func TestAnAgentThatCannotAnswerIsNoInvalidConfiguration(t *testing.T) {
	service, _ := fakeAgentCLI(t, `{"protocol":6,"command":"drupdater:config","ok":false,`+
		`"error":{"kind":"internal","reason":"the agent failed","message":"boom"}}`, 1)

	_, err := service.Config(t.Context(), "/p/.drupdater.yaml")

	require.ErrorContains(t, err, "failed to read the configuration")
	var invalid *InvalidConfigError
	assert.NotErrorAs(t, err, &invalid)
	agentErr, ok := errors.AsType[*agent.Error](err)
	require.True(t, ok, "%v", err)
	assert.Equal(t, agent.Internal, agentErr.Kind)
}
