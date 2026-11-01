package cmd

import (
	"context"

	"go.uber.org/zap"
)

// testApp is productionApp minus trusting checkouts, which writes the global git config.
func testApp() app {
	a := productionApp()
	a.trustCheckout = func(context.Context, *zap.Logger, string) {}
	a.addonCatalog = func(context.Context, *zap.Logger) (addonCatalog, error) { return agentCatalog, nil }
	return a
}
