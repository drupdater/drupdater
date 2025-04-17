package services

import (
	"testing"

	"github.com/drupdater/drupdater/internal/utils"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestGetUpdateHooks(t *testing.T) {

	logger := zap.NewNop()

	t.Run("JSON of updates", func(t *testing.T) {
		data := `{
					"ad_entity_update_8007": {
						"module": "ad_entity",
						"update_id": 8007,
						"description": "8007 - Fix ad_entity.settings due to module uninstalls of sub-modules.",
						"type": "hook_update_n"
					},
					"entity_reference_display_update_8001": {
						"module": "entity_reference_display",
						"update_id": "8001",
						"description": "8001 - Updates the \"negate\" field settings option from integer to boolean.",
						"type": "hook_update_n"
					},
					"menu_link_attributes_update_8002": {
						"module": "menu_link_attributes",
						"update_id": 8002,
						"description": "8002 - Add labels and description to default menu item attributes for clarification.",
						"type": "hook_update_n"
					},
					"migrate_tools_update_10000": {
						"module": "migrate_tools",
						"update_id": "10000",
						"description": "10000 - Adds a table in the database dedicated to SyncSourceIds entries.",
						"type": "hook_update_n"
					}
				}`

		commandExecutor := utils.NewMockCommandExecutor(t)
		commandExecutor.On("ExecDrush", "/tmp", "site1", "updatedb-status", "--format=json").Return(data, nil)

		drush := DefaultDrushService{
			logger:          logger,
			commandExecutor: commandExecutor,
		}

		updates, err := drush.GetUpdateHooks("/tmp", "site1")

		assert.NoError(t, err)

		if len(updates) != 4 {
			t.Errorf("Expected 4 updates, got %d", len(updates))
		}

		assert.Equal(t, map[string]UpdateHook{
			"ad_entity_update_8007": {
				Module:      "ad_entity",
				UpdateID:    float64(8007),
				Description: "8007 - Fix ad_entity.settings due to module uninstalls of sub-modules.",
				Type:        "hook_update_n",
			},
			"entity_reference_display_update_8001": {
				Module:      "entity_reference_display",
				UpdateID:    "8001",
				Description: "8001 - Updates the \"negate\" field settings option from integer to boolean.",
				Type:        "hook_update_n",
			},
			"menu_link_attributes_update_8002": {
				Module:      "menu_link_attributes",
				UpdateID:    float64(8002),
				Description: "8002 - Add labels and description to default menu item attributes for clarification.",
				Type:        "hook_update_n",
			},
			"migrate_tools_update_10000": {
				Module:      "migrate_tools",
				UpdateID:    "10000",
				Description: "10000 - Adds a table in the database dedicated to SyncSourceIds entries.",
				Type:        "hook_update_n",
			},
		}, updates)
	})

	t.Run("No updates", func(t *testing.T) {
		data := ` [success] No database updates required.`

		commandExecutor := utils.NewMockCommandExecutor(t)
		commandExecutor.On("ExecDrush", "/tmp", "site1", "updatedb-status", "--format=json").Return(data, nil)

		drush := DefaultDrushService{
			logger:          logger,
			commandExecutor: commandExecutor,
		}

		updates, err := drush.GetUpdateHooks("/tmp", "site1")

		assert.NoError(t, err)

		if len(updates) != 0 {
			t.Errorf("Expected 0 updates, got %d", len(updates))
		}

		assert.Nil(t, updates)
	})

}
