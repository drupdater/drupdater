package composer

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/drupdater/drupdater/internal/agent"
)

func TestAuditSendsTheRequestFixtureAndDecodesItsReply(t *testing.T) {
	service, sent := fakeAgentCLI(t, agentFixture(t, "audit/response.json"), 0)

	audit, err := service.Audit(t.Context(), t.TempDir())

	require.NoError(t, err)
	request, args := sent()
	assert.JSONEq(t, agentFixture(t, "audit/request.json"), request)
	assert.Equal(t, []string{"composer", "drupdater:audit", "--no-interaction", "--no-ansi"}, args)
	assert.Equal(t, []Advisory{
		{
			ReportedAt:       "2025-01-29T06:52:00+00:00",
			Severity:         "high",
			AdvisoryID:       "PKSA-aaaa-bbbb-cccc",
			CVE:              "CVE-2025-0001",
			Sources:          []Source{{Name: "GitHub", RemoteID: "GHSA-aaaa-bbbb-cccc"}, {Name: "FriendsOfPHP/security-advisories", RemoteID: "fixture/a/CVE-2025-0001.yaml"}},
			Link:             "https://example.com/advisories/CVE-2025-0001",
			PackageName:      "fixture/a",
			AffectedVersions: "<1.1.0",
			Title:            "Remote code execution",
		},
		{
			ReportedAt:       "2025-02-01T00:00:00+00:00",
			AdvisoryID:       "PKSA-dddd-eeee-ffff",
			Sources:          []Source{{Name: "GitHub", RemoteID: "GHSA-dddd-eeee-ffff"}},
			PackageName:      "fixture/a",
			AffectedVersions: ">=1.0.0,<1.0.5",
			Title:            "Cross-site scripting",
		},
	}, audit.Advisories)
	assert.Equal(t, []AbandonedPackage{
		{PackageName: "fixture/gone"},
		{PackageName: "fixture/old", Replacement: "fixture/new"},
	}, audit.Abandoned)
}

func TestAuditOfACleanLockIsEmpty(t *testing.T) {
	service, _ := fakeAgentCLI(t, `{"protocol":1,"command":"drupdater:audit","ok":true,"result":{"advisories":[],"abandoned":[]}}`, 0)

	audit, err := service.Audit(t.Context(), t.TempDir())

	require.NoError(t, err)
	assert.Equal(t, Audit{}, audit)
}

func TestAuditReportsUnreachableAdvisoriesAsUnobtainable(t *testing.T) {
	service, _ := fakeAgentCLI(t, `{"protocol":1,"command":"drupdater:audit","ok":false,`+
		`"error":{"kind":"unobtainable","reason":"the security advisories could not be fetched","message":"HTTP 503"}}`, 1)

	_, err := service.Audit(t.Context(), t.TempDir())

	var agentErr *agent.Error
	require.ErrorAs(t, err, &agentErr)
	assert.Equal(t, agent.Unobtainable, agentErr.Kind)
	assert.ErrorContains(t, err, "failed to audit composer.lock: drupdater:audit: unobtainable (the security advisories could not be fetched): HTTP 503")
}
