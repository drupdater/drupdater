package services

import "go.uber.org/fx"

var Module = fx.Provide(
	fx.Annotate(
		newDrupalSettingsService,
		fx.As(new(SettingsService)),
	),
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
		newDefaultInstallerService,
		fx.As(new(InstallerService)),
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
	fx.Annotate(
		newDefaultDrupalOrgService,
		fx.As(new(DrupalOrgService)),
	),
	fx.Annotate(
		newDefaultDrushService,
		fx.As(new(DrushService)),
	),
)
