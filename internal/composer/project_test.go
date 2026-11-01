package composer

import (
	"encoding/json"
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

	assert.Equal(t, "web", project.WebRoot)
	assert.Equal(t, []string{"web/modules/custom", "web/themes/custom"}, project.CustomCodeDirectories)
	assert.Equal(t, "4b2f0e1c9a7d3e5f6a8b0c1d2e3f4a5b", project.LockHash)
	assert.Equal(t, LockedPackage{Version: "1.7.3"}, project.Packages["cweagans/composer-patches"])
	assert.Equal(t, LockedPackage{Version: "10.3.1"}, project.Packages["drupal/core"])
	assert.Len(t, project.Packages, 6)
	assert.Equal(t, map[string][]string{"drupal/core": {
		"https://www.drupal.org/files/issues/3100000-1.patch",
		"https://www.drupal.org/files/issues/3100001-2.patch",
	}}, project.DependencyPatches)

	var patches map[string]map[string]string
	require.NoError(t, json.Unmarshal(project.Patches, &patches))
	assert.Equal(t, map[string]string{}, patches["drupal/empty"], "an empty patch list stays an object")
	assert.Len(t, patches["drupal/token"], 2)
}

func TestProjectLeavesUnsetSettingsNil(t *testing.T) {
	service, _ := fakeAgentCLI(t, `{"protocol":1,"command":"drupdater:project","ok":true,"result":`+
		`{"web_root":null,"custom_code_directories":[],"lock_hash":null,"packages":{},"dependency_patches":{}}}`, 0)

	project, err := service.Project(t.Context(), t.TempDir())

	require.NoError(t, err)
	assert.Nil(t, project.Patches)
	_, err = project.RequireWebRoot()
	require.EqualError(t, err, "extra.drupal-scaffold.locations.web-root is not defined")
	_, err = project.RequireLockHash()
	require.EqualError(t, err, "composer.lock has no content-hash")
}

func TestProjectReportsTheAgentsFailure(t *testing.T) {
	service, _ := fakeAgentCLI(t, `{"protocol":1,"command":"drupdater:project","ok":false,`+
		`"error":{"kind":"internal","reason":"the agent failed","message":"composer.json is broken"}}`, 1)

	_, err := service.Project(t.Context(), t.TempDir())

	var agentErr *agent.Error
	require.ErrorAs(t, err, &agentErr)
	assert.Equal(t, agent.Internal, agentErr.Kind)
	assert.ErrorContains(t, err, "could not read the project")
}

func TestProjectRequireWebRootAndCustomCode(t *testing.T) {
	project := Project{WebRoot: "docroot", CustomCodeDirectories: []string{"docroot/modules/custom"}}

	webRoot, err := project.RequireWebRoot()
	require.NoError(t, err)
	assert.Equal(t, "docroot", webRoot)
	custom, err := project.CustomCode()
	require.NoError(t, err)
	assert.Equal(t, []string{"docroot/modules/custom"}, custom)

	_, err = Project{CustomCodeDirectories: []string{"x"}}.CustomCode()
	require.ErrorIs(t, err, errNoWebRoot, "without a web root there is nowhere to look for custom code")
}

func TestProjectRequireLockHash(t *testing.T) {
	hash, err := Project{LockHash: "abc"}.RequireLockHash()
	require.NoError(t, err)
	assert.Equal(t, "abc", hash)
}

func TestProjectPackages(t *testing.T) {
	project := Project{Packages: map[string]LockedPackage{
		"drupal/core": {Version: "10.3.1"},
	}}

	assert.True(t, project.Installed("drupal/core"))
	assert.False(t, project.Installed("drupal/token"))
	assert.Equal(t, "10.3.1", project.Version("drupal/core"))
	assert.Empty(t, project.Version("drupal/token"))
}
