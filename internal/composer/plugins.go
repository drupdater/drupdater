package composer

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/drupdater/drupdater/internal/agent"
)

// PluginsOpened is what OpenAllowPlugins did, which CloseAllowPlugins needs to undo it.
type PluginsOpened struct {
	// Original is config.allow-plugins as written; JSON null when composer.json had none.
	Original json.RawMessage `json:"original"`
	// Opened is set when a trailing "*": true rule was added.
	Opened bool `json:"opened"`
}

// OpenAllowPlugins lets the update install any plugin, unless allow-plugins already decides
// every one. The project's own rules, denials included, keep deciding the plugins they name.
func (s *CLI) OpenAllowPlugins(ctx context.Context, dir string) (PluginsOpened, error) {
	opened, err := agent.Call[PluginsOpened](ctx, s.command(dir), "drupdater:allow-plugins", map[string]string{"step": "open"})
	if err != nil {
		return PluginsOpened{}, fmt.Errorf("failed to allow plugins for the update: %w", err)
	}
	return opened, nil
}

type closeAllowPluginsRequest struct {
	Step string `json:"step"`
	PluginsOpened
}

// CloseAllowPlugins denies each locked plugin no rule of the original setting decides, matched
// by Composer's own PluginManager rules, and removes what OpenAllowPlugins added. It returns the
// plugins it denied, sorted.
func (s *CLI) CloseAllowPlugins(ctx context.Context, dir string, opened PluginsOpened) ([]string, error) {
	if opened.Original == nil {
		opened.Original = json.RawMessage("null")
	}
	result, err := agent.Call[struct {
		Denied []string `json:"denied"`
	}](ctx, s.command(dir), "drupdater:allow-plugins", closeAllowPluginsRequest{Step: "close", PluginsOpened: opened})
	if err != nil {
		return nil, fmt.Errorf("failed to close allow-plugins after the update: %w", err)
	}
	return result.Denied, nil
}
