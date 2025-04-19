package services

import (
	"go.uber.org/fx"
)

var Module = fx.Provide(
	fx.Annotate(
		newUpdateTranslations,
		fx.As(new(AfterSiteUpdate)),
		fx.ResultTags(`group:"updater_after_site_update"`),
	),
	fx.Annotate(
		newUpdateCodingStyles,
		fx.As(new(AfterUpdate)),
		fx.ResultTags(`group:"updater_after_update"`),
	),
	fx.Annotate(
		newUpdateRemoveDeprecations,
		fx.As(new(AfterUpdate)),
		fx.ResultTags(`group:"updater_after_update"`),
	),
	fx.Annotate(
		newDefaultUpdater,
		fx.As(new(UpdaterService)),
		fx.ParamTags(`group:"updater_after_site_update"`),
	),
	fx.Annotate(
		NewGitRepositoryService,
		fx.As(new(RepositoryService)),
	),
	fx.Annotate(
		NewDependencyUpdateStrategy,
		fx.ParamTags(`group:"updater_after_update"`),
	),
	fx.Annotate(
		NewSecurityUpdateStrategy,
	),
	fx.Annotate(
		NewWorkflowBaseService,
		fx.As(new(WorkflowService)),
	),
)
