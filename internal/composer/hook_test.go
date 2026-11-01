package composer

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/drupdater/drupdater/internal/agent"
)

func TestAddonsSendsTheRequestFixtureAndDecodesTheCatalogue(t *testing.T) {
	service, sent := fakeAgentCLI(t, agentFixture(t, "addons/response.json"), 0)

	addons, err := service.Addons(t.Context())

	require.NoError(t, err)
	request, args := sent()
	assert.JSONEq(t, agentFixture(t, "addons/request.json"), request)
	assert.Equal(t, []string{"composer", "drupdater:addons", "--no-interaction", "--no-ansi"}, args)
	require.Len(t, addons, 10)
	assert.Equal(t, agent.Addon{
		Name:      "composer_allow_plugins",
		Mandatory: true,
		Hooks:     map[string]string{"pre-composer-update": "change", "post-composer-update": "change"},
	}, addons[0])
	assert.Equal(t, agent.Addon{Name: "code_beautifier", Default: true, Hooks: map[string]string{"post-code-update": "format"}}, addons[6])
}

func TestAddonsReportsAnAgentThatCannotAnswer(t *testing.T) {
	service, _ := fakeAgentCLI(t, "Command \"drupdater:addons\" is not defined.", 1)

	_, err := service.Addons(t.Context())

	require.ErrorContains(t, err, "failed to read the addon catalogue")
}

func TestRunHookSendsTheRequestFixtureAndDecodesItsReply(t *testing.T) {
	service, sent := fakeAgentCLI(t, agentFixture(t, "hook/response.json"), 0)
	var request struct {
		Request agent.HookRequest `json:"request"`
	}
	require.NoError(t, json.Unmarshal([]byte(agentFixture(t, "hook/request.json")), &request))

	reply, err := service.RunHook(t.Context(), t.TempDir(), request.Request)

	require.NoError(t, err)
	body, args := sent()
	assert.JSONEq(t, agentFixture(t, "hook/request.json"), body)
	assert.Equal(t, []string{"composer", "drupdater:hook", "--no-interaction", "--no-ansi"}, args)
	assert.Empty(t, reply.Commits)
	assert.Nil(t, reply.Plan)
	assert.Empty(t, reply.Abort)
	var rows []map[string]any
	require.NoError(t, json.Unmarshal(reply.Finding, &rows))
	assert.Equal(t, []any{"fixture/a", "fixture/gone", "fixture/new", "fixture/tool"},
		[]any{rows[0]["package"], rows[1]["package"], rows[2]["package"], rows[3]["package"]})
}

func TestRunHookReportsTheAgentsRefusal(t *testing.T) {
	service, _ := fakeAgentCLI(t, `{"protocol":3,"command":"drupdater:hook","ok":false,`+
		`"error":{"kind":"invalid_request","reason":"no addon \"nope\" is in the catalogue","message":""}}`, 1)

	_, err := service.RunHook(t.Context(), t.TempDir(), agent.HookRequest{Addon: "nope"})

	agentErr, ok := errors.AsType[*agent.Error](err)
	require.True(t, ok, "%v", err)
	assert.Equal(t, agent.InvalidRequest, agentErr.Kind)
}

func TestReportSendsTheRequestFixtureAndDecodesTheSections(t *testing.T) {
	service, sent := fakeAgentCLI(t, agentFixture(t, "report/response.json"), 0)
	var request struct {
		Request agent.ReportRequest `json:"request"`
	}
	require.NoError(t, json.Unmarshal([]byte(agentFixture(t, "report/request.json")), &request))

	reply, err := service.Report(t.Context(), request.Request)

	require.NoError(t, err)
	body, _ := sent()
	assert.JSONEq(t, agentFixture(t, "report/request.json"), body)
	assert.Len(t, reply.Sections, 9)
	assert.JSONEq(t, `{"added":["php-http/discovery","tbachert/spi"]}`, string(reply.Sections["composer_allow_plugins"]))
}

func TestReportReportsTheAgentsFailure(t *testing.T) {
	service, _ := fakeAgentCLI(t, `{"protocol":3,"command":"drupdater:report","ok":false,`+
		`"error":{"kind":"internal","reason":"the agent failed","message":"boom"}}`, 1)

	_, err := service.Report(t.Context(), agent.ReportRequest{})

	require.ErrorContains(t, err, "failed to build the addons' report sections")
}
