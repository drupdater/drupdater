package codehosting

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// The repository URL arrives from a CI variable or a flag and is parsed by hand, because the SCP
// form url.Parse does not accept is the one most CI systems export. The property test generates
// well-formed URLs; fuzzing covers what an operator actually mistypes.
func FuzzParseGitURL(f *testing.F) {
	for _, seed := range []string{
		"https://github.com/drupdater/drupdater.git",
		"git@gitlab.com:group/subgroup/project.git",
		"ssh://git@example.com:2222/owner/repo.git",
		"https://oauth2:token@gitlab.example.com:8443/owner/repo",
		"",
		"://",
		"h:",
		":/",
		"\x00",
		strings.Repeat("@", 32),
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, raw string) {
		host, path, err := parseGitURL(raw)
		if err != nil {
			assert.Empty(t, host)
			assert.Empty(t, path)
			return
		}

		assert.NotEmpty(t, host, "a URL that parsed has to name the host the token is sent to")
		assert.False(t, strings.HasSuffix(path, ".git"), "path %q kept its .git suffix", path)
		assert.False(t, strings.HasPrefix(path, "/"), "path %q kept a leading slash", path)
		assert.False(t, strings.HasSuffix(path, "/"), "path %q kept a trailing slash", path)

		// ValidateRepositoryURL is the preflight check. Disagreeing means a check passes and
		// the run then fails on the very URL it just approved.
		assert.NoError(t, ValidateRepositoryURL(raw))
	})
}
