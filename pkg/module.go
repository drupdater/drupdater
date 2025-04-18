package pkg

import (
	"github.com/drupdater/drupdater/pkg/composer"
	"go.uber.org/fx"
)

var Module = fx.Provide(
	fx.Annotate(
		composer.NewDefaultComposerService,
		fx.As(new(composer.ComposerService)),
	),
)
