package cmd

import (
	"context"
	"errors"
	"os"
	"slices"
	"time"

	"go.uber.org/zap"

	"github.com/drupdater/drupdater/internal/agent"
	"github.com/drupdater/drupdater/internal/composer"
)

// testApp is productionApp minus trusting checkouts, which writes the global git config, and with
// an agent that answers from agentCatalog: no .drupdater.yaml reads as the defaults, and any file
// is refused, so a test that needs a file's values sets projectConfig itself.
func testApp() app {
	a := productionApp()
	a.trustCheckout = func(context.Context, *zap.Logger, string) {}
	a.addonCatalog = func(context.Context, *zap.Logger) (addonCatalog, error) { return agentCatalog, nil }
	a.projectConfig = func(ctx context.Context, _ *zap.Logger, path string) (agent.ConfigReply, error) {
		if ctx == nil {
			return agent.ConfigReply{}, errNoContext
		}
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			return defaultProjectConfig(), nil
		}
		return agent.ConfigReply{}, &composer.InvalidConfigError{Reason: "in " + path + `: unknown key "bogus"`}
	}
	return a
}

// errNoContext is what the test agent answers a call without the command's context, which an
// interrupt could not reach.
var errNoContext = errors.New("asked without a context")

// defaultProjectConfig is what drupdater:config answers without a file, for agentCatalog.
func defaultProjectConfig() agent.ConfigReply {
	reply := agent.ConfigReply{Sites: []string{"default"}, TimeoutNS: int64(30 * time.Minute)}
	reply.RunTypes.Normal.Addons = slices.Concat(agentCatalog.mandatory, agentCatalog.defaults)
	reply.RunTypes.Security.Addons = slices.Clone(agentCatalog.mandatory)
	return reply
}

// answering is an app whose agent answers every .drupdater.yaml with reply.
func answering(a app, reply agent.ConfigReply, asked *string) app {
	a.projectConfig = func(ctx context.Context, _ *zap.Logger, path string) (agent.ConfigReply, error) {
		if ctx == nil {
			return agent.ConfigReply{}, errNoContext
		}
		if asked != nil {
			*asked = path
		}
		return reply, nil
	}
	return a
}
