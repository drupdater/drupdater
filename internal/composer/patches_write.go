package composer

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/drupdater/drupdater/internal/agent"
)

// WritePatches writes patches, a JSON object, as extra.patches and brings composer.lock's
// content-hash in line, resolving nothing.
func (s *CLI) WritePatches(ctx context.Context, dir string, patches json.RawMessage) error {
	_, err := agent.Call[struct{}](ctx, s.command(dir), "drupdater:write-patches", struct {
		Patches json.RawMessage `json:"patches"`
	}{patches})
	if err != nil {
		return fmt.Errorf("failed to write the patches: %w", err)
	}
	return nil
}
