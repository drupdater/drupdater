package composer

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/drupdater/drupdater/internal/agent"
)

// fixtureDataDir is the data_dir the agent/protocol site requests name.
const fixtureDataDir = "/tmp/drupdater-sites-fixture"

func fixtureFacts(t *testing.T, name string) string {
	t.Helper()
	var reply struct {
		Result struct {
			Facts json.RawMessage `json:"facts"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal([]byte(agentFixture(t, name)), &reply))
	return string(reply.Result.Facts)
}

func TestSiteFactsAskDrupalOrgAndComeBackAsTheAgentReadThem(t *testing.T) {
	service, sent := fakeAgentCLI(t, agentFixture(t, "site-facts/response.json"), 0)

	facts, err := service.Sites(fixtureDataDir).Facts(t.Context(), t.TempDir(), "default")

	require.NoError(t, err)
	assert.JSONEq(t, fixtureFacts(t, "site-facts/response.json"), string(facts))
	request, args := sent()
	assert.JSONEq(t, agentFixture(t, "site-facts/request.json"), request)
	assert.Equal(t, []string{"composer", "drupdater:site-facts", "--no-interaction", "--no-ansi"}, args)
}

func TestSiteUpdateReturnsTheFactsAfterwards(t *testing.T) {
	service, sent := fakeAgentCLI(t, agentFixture(t, "site-update/response.json"), 0)

	facts, err := service.Sites(fixtureDataDir).Update(t.Context(), t.TempDir(), "default")

	require.NoError(t, err)
	assert.JSONEq(t, fixtureFacts(t, "site-update/response.json"), string(facts))
	assert.Contains(t, string(facts), `"update_hooks":{}`, "handed on byte for byte, so an empty object stays one")
	request, _ := sent()
	assert.JSONEq(t, agentFixture(t, "site-update/request.json"), request)
}

func TestFactsThatAreNoObjectAreRefused(t *testing.T) {
	for name, reply := range map[string]string{
		"missing": `{"protocol":4,"command":"drupdater:site-facts","ok":true,"result":{}}`,
		"null":    `{"protocol":4,"command":"drupdater:site-facts","ok":true,"result":{"facts":null}}`,
		"a list":  `{"protocol":4,"command":"drupdater:site-facts","ok":true,"result":{"facts":[]}}`,
	} {
		t.Run(name, func(t *testing.T) {
			service, _ := fakeAgentCLI(t, reply, 0)

			_, err := service.Sites(fixtureDataDir).Facts(t.Context(), t.TempDir(), "default")

			require.EqualError(t, err, "failed to read the facts of site default: the agent answered no facts")
		})
	}
}

func TestSiteCallsNameTheSiteTheyFailedFor(t *testing.T) {
	failure := func(command string) string {
		return `{"protocol":4,"command":"` + command + `","ok":false,"error":{"kind":"failed","reason":"drush updatedb --yes failed","message":""}}`
	}
	for _, tt := range []struct {
		command string
		call    func(*Sites) error
		want    string
	}{
		{"drupdater:site-facts", func(s *Sites) error {
			_, err := s.Facts(t.Context(), t.TempDir(), "default")
			return err
		}, "failed to read the facts of site default"},
		{"drupdater:site-update", func(s *Sites) error {
			_, err := s.Update(t.Context(), t.TempDir(), "default")
			return err
		}, "failed to update site default"},
		{"drupdater:config-export", func(s *Sites) error {
			_, err := s.ExportConfiguration(t.Context(), t.TempDir(), "default")
			return err
		}, "failed to export the configuration of site default"},
	} {
		t.Run(tt.command, func(t *testing.T) {
			service, _ := fakeAgentCLI(t, failure(tt.command), 1)

			err := tt.call(service.Sites(fixtureDataDir))

			var agentErr *agent.Error
			require.ErrorAs(t, err, &agentErr)
			assert.ErrorContains(t, err, tt.want+": "+tt.command+": failed (drush updatedb --yes failed)")
		})
	}
}

func TestConfigExportNamesTheDirectoryItWrote(t *testing.T) {
	service, sent := fakeAgentCLI(t, agentFixture(t, "config-export/response.json"), 0)

	path, err := service.Sites(fixtureDataDir).ExportConfiguration(t.Context(), t.TempDir(), "default")

	require.NoError(t, err)
	assert.Equal(t, "config/sync", path)
	request, _ := sent()
	assert.JSONEq(t, agentFixture(t, "config-export/request.json"), request)
}

func TestTheDatabaseCallsSendTheSiteAndTheDataDirectory(t *testing.T) {
	for _, tt := range []struct {
		fixture string
		call    func(s *Sites, dir string) error
	}{
		{"site-install", func(s *Sites, dir string) error { return s.Install(t.Context(), dir, "default") }},
		{"site-database", func(s *Sites, dir string) error { return s.ConfigureDatabase(t.Context(), dir, "default") }},
		{"site-restore", func(s *Sites, dir string) error { return s.RestoreSettings(t.Context(), dir, "default") }},
	} {
		t.Run(tt.fixture, func(t *testing.T) {
			service, sent := fakeAgentCLI(t, agentFixture(t, tt.fixture+"/response.json"), 0)

			require.NoError(t, tt.call(service.Sites(fixtureDataDir), t.TempDir()))
			request, args := sent()
			assert.JSONEq(t, agentFixture(t, tt.fixture+"/request.json"), request)
			assert.Equal(t, []string{"composer", "drupdater:" + tt.fixture, "--no-interaction", "--no-ansi"}, args)
		})
	}
}

func TestADatabaseCallReportsTheAgentsFailure(t *testing.T) {
	service, _ := fakeAgentCLI(t, `{"protocol":4,"command":"drupdater:site-install","ok":false,`+
		`"error":{"kind":"failed","reason":"failed to read settings file","message":""}}`, 1)

	err := service.Sites(fixtureDataDir).Install(t.Context(), t.TempDir(), "default")

	var agentErr *agent.Error
	require.ErrorAs(t, err, &agentErr)
	assert.Equal(t, "failed to read settings file", agentErr.Reason)
}
