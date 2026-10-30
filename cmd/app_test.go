package cmd

import (
	"context"

	"go.uber.org/zap"
)

// testApp is productionApp minus trusting checkouts, which writes the global git config.
func testApp() app {
	a := productionApp()
	a.trustCheckout = func(context.Context, *zap.Logger, string) {}
	return a
}
