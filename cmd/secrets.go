package cmd

import (
	"encoding/json"
	"errors"
	"os"

	"github.com/drupdater/drupdater/internal/config"
	"github.com/drupdater/drupdater/internal/logging"
)

// resolveToken takes the argument, else DRUPDATER_TOKEN. Mandatory only when the run will use it.
func resolveToken(args []string, cfg config.Config) (string, error) {
	var token string
	if len(args) == 1 && args[0] != "" {
		token = args[0]
	} else {
		token = os.Getenv("DRUPDATER_TOKEN")
	}
	if token == "" && tokenRequired(cfg) {
		return "", errors.New("no token provided: pass it as the argument or set DRUPDATER_TOKEN")
	}
	return token, nil
}

// tokenRequired reports whether this run clones or publishes. A checkout-mode --dry-run does neither.
func tokenRequired(cfg config.Config) bool {
	return cfg.Clone || !cfg.DryRun
}

// registerEnvSecrets registers every credential-bearing environment value a subprocess may echo.
func registerEnvSecrets(redactor *logging.Redactor) {
	redactor.Register(os.Getenv("DRUPALCODE_ACCESS_TOKEN"))
	registerComposerAuth(redactor, os.Getenv("COMPOSER_AUTH"))
}

// registerComposerAuth registers the credentials inside a COMPOSER_AUTH value. Composer echoes the
// individual leaf, not the JSON blob. The raw value goes in too, for when it does not parse.
func registerComposerAuth(redactor *logging.Redactor, composerAuth string) {
	redactor.Register(composerAuth)

	var parsed any
	if err := json.Unmarshal([]byte(composerAuth), &parsed); err != nil {
		return
	}
	redactor.Register(composerAuthSecretLeaves(parsed, "")...)
}

// composerAuthSecretLeaves returns every string value in a decoded COMPOSER_AUTH structure.
func composerAuthSecretLeaves(v any, key string) []string {
	switch t := v.(type) {
	case string:
		// http-basic commonly sets username to the literal "token", which must stay readable.
		if key == "username" {
			return nil
		}
		return []string{t}
	case map[string]any:
		leaves := make([]string, 0, len(t))
		for k, val := range t {
			leaves = append(leaves, composerAuthSecretLeaves(val, k)...)
		}
		return leaves
	case []any:
		leaves := make([]string, 0, len(t))
		for _, val := range t {
			leaves = append(leaves, composerAuthSecretLeaves(val, key)...)
		}
		return leaves
	default:
		return nil
	}
}
