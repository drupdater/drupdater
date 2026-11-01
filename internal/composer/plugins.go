package composer

import (
	"regexp"
	"strings"
)

// AllowPlugins is composer.json's config.allow-plugins as the project wrote it.
type AllowPlugins struct {
	// Set is false when composer.json has no allow-plugins at all.
	Set bool
	// All is the value when it is a boolean, which decides every plugin at once.
	All *bool
	// Rules maps package patterns, in which * matches anything, to whether they may run.
	Rules map[string]bool
}

// Decides reports whether the setting already allows or denies plugin, matching patterns the way
// Composer's PluginManager does: anchored, case-insensitive, * for any characters.
func (a AllowPlugins) Decides(plugin string) bool {
	if a.All != nil {
		return true
	}
	for pattern := range a.Rules {
		re := "(?i)^" + strings.ReplaceAll(regexp.QuoteMeta(pattern), `\*`, ".*") + "$"
		if regexp.MustCompile(re).MatchString(plugin) {
			return true
		}
	}
	return false
}
