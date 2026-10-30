package addon

import (
	"context"
	"fmt"

	"github.com/drupdater/drupdater/internal/workflow"
	"github.com/drupdater/drupdater/pkg/drush"
	"go.uber.org/zap"
)

// UpdateHooksDrush lists a site's pending update hooks.
type UpdateHooksDrush interface {
	GetUpdateHooks(ctx context.Context, dir string, site string) (map[string]drush.UpdateHook, error)
}

type UpdateHooks struct {
	logger *zap.Logger
	drush  UpdateHooksDrush

	hooks workflow.PerSite[map[string]drush.UpdateHook]
}

func NewUpdateHooks(logger *zap.Logger, drush UpdateHooksDrush) *UpdateHooks {
	return &UpdateHooks{
		logger: logger,
		drush:  drush,
	}
}

func (uh *UpdateHooks) Hooks() workflow.Hooks {
	return workflow.Hooks{
		PreSiteUpdate: workflow.On(workflow.Inspect, uh.preSiteUpdateHandler),
	}
}

func (uh *UpdateHooks) preSiteUpdateHandler(evt *workflow.PreSiteUpdateEvent) error {

	hooks, err := uh.drush.GetUpdateHooks(evt.Context(), evt.Path(), evt.Site())
	uh.logger.Debug("update hooks", zap.Any("hooks", hooks))
	if err != nil {
		return fmt.Errorf("failed to get update hooks: %w", err)
	}
	if len(hooks) == 0 {
		uh.logger.Debug("no update hooks found")
		return nil
	}
	uh.hooks.Set(evt.Site(), hooks)
	uh.logger.Info("update hooks found", zap.String("site", evt.Site()), zap.Int("count", len(hooks)))

	return nil
}
