package codehosting

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

// Repository URLs arrive from a CI variable or a flag, in whichever of the two shapes git
// itself accepts. The example-based tests cover the shapes someone wrote down; these state what
// has to hold for every URL of either shape, and that the parser is total — it is called on
// unvalidated user input, so "never panics" is part of the contract.

// hostGen generates a repository host: labels of the kind a real forge uses. No port — the SCP
// form has no slot for one, since the colon after the host already introduces the path, so a
// port belongs only to the URL form and is covered separately.
func hostGen() *rapid.Generator[string] {
	return rapid.StringMatching(`[a-z][a-z0-9-]{0,10}(\.[a-z][a-z0-9-]{0,10}){1,2}`)
}

// segmentGen generates one path segment of an "owner/repo" pair.
func segmentGen() *rapid.Generator[string] {
	return rapid.StringMatching(`[a-zA-Z0-9][a-zA-Z0-9._-]{0,15}`)
}

func TestPropertyParseGitURLRoundTripsBothURLShapes(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		host := hostGen().Draw(t, "host")
		owner := segmentGen().Draw(t, "owner")
		repo := segmentGen().Draw(t, "repo")
		wantPath := owner + "/" + repo

		// Both forms name the same repository, so both have to parse back to the same pair.
		// The SCP form is the one url.ParseRequestURI rejects outright, which is why
		// ValidateRepositoryURL exists.
		for _, raw := range []string{
			"https://" + host + "/" + wantPath + ".git",
			"https://" + host + "/" + wantPath,
			"git@" + host + ":" + wantPath + ".git",
			"git@" + host + ":" + wantPath,
		} {
			gotHost, gotPath, err := parseGitURL(raw)
			require.NoError(t, err, "parsing %q", raw)
			assert.Equal(t, host, gotHost, "host from %q", raw)
			assert.Equal(t, wantPath, gotPath, "path from %q", raw)
		}
	})
}

func TestPropertyParseGitURLKeepsThePort(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		host := hostGen().Draw(t, "host")
		port := rapid.IntRange(1, 65535).Draw(t, "port")
		owner := segmentGen().Draw(t, "owner")
		repo := segmentGen().Draw(t, "repo")

		// A self-hosted GitLab is routinely reached on a non-default port, and the port is part
		// of the host the gitlab client is constructed with — dropping it would send every API
		// call somewhere else.
		hostPort := fmt.Sprintf("%s:%d", host, port)
		gotHost, gotPath, err := parseGitURL("https://" + hostPort + "/" + owner + "/" + repo + ".git")
		require.NoError(t, err)
		assert.Equal(t, hostPort, gotHost)
		assert.Equal(t, owner+"/"+repo, gotPath)
	})
}

func TestPropertyParseGitURLNormalisesItsOutput(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		raw := rapid.String().Draw(t, "raw")

		// Total function: parseGitURL runs on whatever the operator passed in, so every input
		// either parses or returns an error, and none of them panics.
		host, path, err := parseGitURL(raw)
		if err != nil {
			assert.Empty(t, host)
			assert.Empty(t, path)
			return
		}

		assert.NotEmpty(t, host, "a URL that parsed must have a host")
		assert.False(t, strings.HasSuffix(path, ".git"), "path %q kept its .git suffix", path)
		assert.False(t, strings.HasPrefix(path, "/"), "path %q kept a leading slash", path)
		assert.False(t, strings.HasSuffix(path, "/"), "path %q kept a trailing slash", path)
	})
}

func TestPropertyValidateRepositoryURLAgreesWithParseGitURL(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		raw := rapid.String().Draw(t, "raw")

		// The whole point of ValidateRepositoryURL is that a preflight check accepts exactly
		// what a run will later accept. If the two ever diverged, a check would pass and the
		// run would then fail on the same URL.
		_, _, parseErr := parseGitURL(raw)
		assert.Equal(t, parseErr == nil, ValidateRepositoryURL(raw) == nil, "for %q", raw)
	})
}

func TestPropertyProviderFromHostIgnoresCase(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		host := rapid.StringMatching(`[a-zA-Z0-9.-]{0,30}`).Draw(t, "host")
		upper := rapid.SliceOfN(rapid.Bool(), len(host), len(host)).Draw(t, "upper")

		// Hostnames are case-insensitive, and a CI variable may well carry one in mixed case.
		// Which client a repository is routed to must not depend on that.
		var flipped strings.Builder
		for i, r := range []byte(host) {
			if upper[i] {
				flipped.WriteString(strings.ToUpper(string(r)))
				continue
			}
			flipped.WriteString(strings.ToLower(string(r)))
		}

		assert.Equal(t, providerFromHost(host), providerFromHost(flipped.String()))
	})
}

func TestPropertyProviderFromHostPrefersGitlab(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		prefix := rapid.StringMatching(`[a-z.-]{0,8}`).Draw(t, "prefix")
		middle := rapid.StringMatching(`[a-z.-]{0,8}`).Draw(t, "middle")
		suffix := rapid.StringMatching(`[a-z.-]{0,8}`).Draw(t, "suffix")

		// A host naming both is ambiguous and the precedence is load-bearing: gitlab wins,
		// whichever order the two appear in. Create falls through to gitlab for "" as well, so
		// the only host that reaches the github client is one that says github and not gitlab.
		gitlabFirst := prefix + "gitlab" + middle + "github" + suffix
		githubFirst := prefix + "github" + middle + "gitlab" + suffix

		assert.Equal(t, "gitlab", providerFromHost(gitlabFirst))
		assert.Equal(t, "gitlab", providerFromHost(githubFirst))
	})
}
