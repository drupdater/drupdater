package pkg

import (
	"github.com/drupdater/drupdater/pkg/composer"
	"github.com/drupdater/drupdater/pkg/drush"
	"github.com/drupdater/drupdater/pkg/phpcs"
	"github.com/drupdater/drupdater/pkg/rector"
	"go.uber.org/fx"
)

var Module = fx.Provide(
	fx.Annotate(
		composer.NewDefaultComposerService,
		fx.As(new(composer.ComposerService)),
	),
	fx.Annotate(
		phpcs.NewDefaultPhpCsService,
		fx.As(new(phpcs.PhpCsService)),
	),
	fx.Annotate(
		rector.NewDefaultRectorService,
		fx.As(new(rector.RectorService)),
	),
	fx.Annotate(
		drush.NewDefaultDrushService,
		fx.As(new(drush.DrushService)),
	),
)
