package cmd

import (
	"context"

	"go.uber.org/zap"
)

// testApp is productionApp without its one write outside the test: trusting a checkout in the
// global git config, which every checkout-mode run would otherwise add a temp dir to.
func testApp() app {
	a := productionApp()
	a.trustCheckout = func(context.Context, *zap.Logger, string) {}
	return a
}
