package addon

import (
	"github.com/drupdater/drupdater/internal/drush"
	"github.com/drupdater/drupdater/internal/workflow"
	"go.uber.org/zap"
)

type UpdateHooks struct {
	logger *zap.Logger
}

func NewUpdateHooks(logger *zap.Logger) *UpdateHooks {
	return &UpdateHooks{
		logger: logger,
	}
}

func (uh *UpdateHooks) Hooks() workflow.Hooks {
	return workflow.Hooks{
		PreSiteUpdate: workflow.Observe(uh.preSiteUpdateHandler),
	}
}

func (uh *UpdateHooks) preSiteUpdateHandler(evt *workflow.PreSiteUpdateEvent) (map[string]drush.UpdateHook, error) {
	hooks := evt.Facts().UpdateHooks
	uh.logger.Debug("update hooks", zap.Any("hooks", hooks))
	if len(hooks) == 0 {
		uh.logger.Debug("no update hooks found")
		return hooks, nil
	}
	uh.logger.Info("update hooks found", zap.String("site", evt.Site()), zap.Int("count", len(hooks)))

	return hooks, nil
}
