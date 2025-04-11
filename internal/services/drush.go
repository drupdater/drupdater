package services

import (
	"encoding/json"
	"strings"

	"ebersolve.com/updater/internal/utils"
	"go.uber.org/zap"
)

type DrushService interface {
	GetUpdateHooks(dir string, site string) (map[string]UpdateHook, error)
}

type DefaultDrushService struct {
	logger          *zap.Logger
	commandExecutor utils.CommandExecutor
}

func newDefaultDrushService(logger *zap.Logger, commandExecutor utils.CommandExecutor) *DefaultDrushService {
	return &DefaultDrushService{
		logger:          logger,
		commandExecutor: commandExecutor,
	}
}

type UpdateHook struct {
	Module      string      `json:"module"`
	UpdateID    interface{} `json:"update_id"`
	Description string      `json:"description"`
	Type        string      `json:"type"`
}

func (s *DefaultDrushService) GetUpdateHooks(dir string, site string) (map[string]UpdateHook, error) {
	s.logger.Debug("getting update hooks")
	data, err := s.commandExecutor.ExecDrush(dir, site, "updatedb-status", "--format=json")
	if err != nil {
		return nil, err
	}

	if strings.Contains(data, "No database updates required") {
		return nil, nil
	}

	var updates map[string]UpdateHook
	if err := json.Unmarshal([]byte(data), &updates); err != nil {
		s.logger.Error("failed to unmarshal update hooks", zap.Error(err))
		return nil, err
	}

	return updates, nil
}
