package codehosting

import "go.uber.org/fx"

var Module = fx.Provide(
	fx.Annotate(
		NewDefaultVcsProviderFactory,
		fx.As(new(VcsProviderFactory)),
	),
)
