package pkg

import (
	"github.com/drupdater/drupdater/pkg/composer"
	"github.com/drupdater/drupdater/pkg/drupalorg"
	"github.com/drupdater/drupdater/pkg/drush"
	"github.com/drupdater/drupdater/pkg/phpcs"
	"github.com/drupdater/drupdater/pkg/rector"
	"go.uber.org/fx"
)

var Module = fx.Provide(
	fx.Annotate(
		composer.NewCLI,
		fx.As(new(composer.Runner)),
	),
	fx.Annotate(
		phpcs.NewCLI,
		fx.As(new(phpcs.Runner)),
	),
	fx.Annotate(
		rector.NewCLI,
		fx.As(new(rector.Runner)),
	),
	fx.Annotate(
		drush.NewCLI,
		fx.As(new(drush.Runner)),
	),
	fx.Annotate(
		drupalorg.NewHTTPClient,
		fx.As(new(drupalorg.Client)),
	),
)
