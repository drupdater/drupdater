package composer

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/drupdater/drupdater/internal/agent"
)

// appFixture returns one of app/protocol's examples, compacted to the single line the app prints.
func appFixture(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "app", "protocol", name))
	require.NoError(t, err)
	var compact bytes.Buffer
	require.NoError(t, json.Compact(&compact, raw))
	return compact.String()
}

func TestFireSendsTheRequestFixtureToTheAppAndDecodesItsReply(t *testing.T) {
	service, sent := fakeAgentCLI(t, appFixture(t, "fire/response.json"), 0)
	request := agent.FireRequest{
		Event: agent.Event{Name: "pre-composer-update", Plan: &agent.Plan{Only: []string{}, Keep: []agent.Pin{}}},
		Addons: []agent.FireAddon{
			{Name: "composer_patches", Hooks: map[string]string{"pre-composer-update": "change", "post-composer-update": "change"}, Findings: []agent.Finding{}},
			{Name: "composer_audit", Hooks: map[string]string{"pre-composer-update": "prepare"}, Findings: []agent.Finding{
				{Event: "post-site-update", Site: "default", Finding: json.RawMessage(`{}`), Committed: []string{}},
			}},
		},
		Run:    agent.Run{Security: true},
		Author: agent.Author{Name: "drupdater", Email: "drupdater@localhost"},
	}

	reply, err := service.Fire(t.Context(), t.TempDir(), request)

	require.NoError(t, err)
	body, args := sent()
	assert.JSONEq(t, appFixture(t, "fire/request.json"), body)
	assert.Equal(t, []string{AppPath, "internal:fire", "--no-interaction", "--no-ansi"}, args)
	assert.Equal(t, agent.FireReply{
		Recorded: []agent.Recorded{
			{Addon: "composer_audit", Finding: agent.Finding{Event: "pre-composer-update", Finding: json.RawMessage(`{"advisories":1}`), Committed: []string{}}},
			{Addon: "composer_patches", Finding: agent.Finding{Event: "pre-composer-update", Finding: json.RawMessage(`null`), Committed: []string{"Pin drupal/core"}}},
		},
		Plan: &agent.Plan{Only: []string{"drupal/core"}, Keep: []agent.Pin{{Package: "drupal/core", Version: "10.3.1"}}, MinimalChanges: true},
	}, reply)
}

func TestFireDecodesAFailedEvent(t *testing.T) {
	service, _ := fakeAgentCLI(t, appFixture(t, "fire/failed.json"), 0)

	reply, err := service.Fire(t.Context(), t.TempDir(), agent.FireRequest{})

	require.NoError(t, err)
	assert.Nil(t, reply.Plan)
	assert.Len(t, reply.Recorded, 1)
	assert.Equal(t, "pre-composer-update hook failed: composer_patches: drupdater:hook: failed (the patch does not apply)", reply.Failure)
}

func TestCommitSendsTheRequestFixtureToTheAppAndDecodesItsReply(t *testing.T) {
	service, sent := fakeAgentCLI(t, appFixture(t, "commit/response.json"), 0)

	reply, err := service.Commit(t.Context(), t.TempDir(), agent.CommitRequest{
		Author: agent.Author{Name: "drupdater", Email: "drupdater@localhost"},
		Commits: []agent.Commit{
			{Message: "Update configuration default", Paths: []string{"config/sync"}},
			{Message: "Update configuration second", Paths: []string{"config/second"}},
		},
	})

	require.NoError(t, err)
	body, args := sent()
	assert.JSONEq(t, appFixture(t, "commit/request.json"), body)
	assert.Equal(t, []string{AppPath, "internal:commit", "--no-interaction", "--no-ansi"}, args)
	assert.Equal(t, agent.CommitReply{Committed: []string{"Update configuration default"}}, reply)
}

func TestCommitKeepsTheAppsFailure(t *testing.T) {
	service, _ := fakeAgentCLI(t, `{"protocol":6,"command":"internal:commit","ok":false,`+
		`"error":{"kind":"failed","reason":"the commit failed","message":"refusing"}}`, 1)

	_, err := service.Commit(t.Context(), t.TempDir(), agent.CommitRequest{})

	appErr, ok := errors.AsType[*agent.Error](err)
	require.True(t, ok, "%v", err)
	assert.Equal(t, agent.Failed, appErr.Kind)
}
