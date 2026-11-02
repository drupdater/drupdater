package composer

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/drupdater/drupdater/internal/agent"
)

func TestProjectDecodesTheResponseFixture(t *testing.T) {
	service, sent := fakeAgentCLI(t, agentFixture(t, "project/response.json"), 0)

	project, err := service.Project(t.Context(), t.TempDir())

	require.NoError(t, err)
	request, args := sent()
	assert.JSONEq(t, agentFixture(t, "project/request.json"), request)
	assert.Equal(t, []string{"composer", "drupdater:project", "--no-interaction", "--no-ansi"}, args)

	assert.Equal(t, Project{WebRoot: "web", LockHash: "4b2f0e1c9a7d3e5f6a8b0c1d2e3f4a5b"}, project)
}

func TestProjectLeavesUnsetSettingsNil(t *testing.T) {
	service, _ := fakeAgentCLI(t, `{"protocol":3,"command":"drupdater:project","ok":true,"result":`+
		`{"web_root":null,"lock_hash":null}}`, 0)

	project, err := service.Project(t.Context(), t.TempDir())

	require.NoError(t, err)
	_, err = project.RequireWebRoot()
	require.EqualError(t, err, "extra.drupal-scaffold.locations.web-root is not defined")
	_, err = project.RequireLockHash()
	require.EqualError(t, err, "composer.lock has no content-hash")
}

func TestProjectReportsTheAgentsFailure(t *testing.T) {
	service, _ := fakeAgentCLI(t, `{"protocol":3,"command":"drupdater:project","ok":false,`+
		`"error":{"kind":"internal","reason":"the agent failed","message":"composer.json is broken"}}`, 1)

	_, err := service.Project(t.Context(), t.TempDir())

	var agentErr *agent.Error
	require.ErrorAs(t, err, &agentErr)
	assert.Equal(t, agent.Internal, agentErr.Kind)
	assert.ErrorContains(t, err, "could not read the project")
}

func TestProjectRequireWebRoot(t *testing.T) {
	webRoot, err := Project{WebRoot: "docroot"}.RequireWebRoot()
	require.NoError(t, err)
	assert.Equal(t, "docroot", webRoot)
}

func TestProjectRequireLockHash(t *testing.T) {
	hash, err := Project{LockHash: "abc"}.RequireLockHash()
	require.NoError(t, err)
	assert.Equal(t, "abc", hash)
}
