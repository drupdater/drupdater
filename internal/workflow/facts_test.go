package workflow

import "github.com/drupdater/drupdater/internal/drush"

// syncFacts is a site whose facts matter to the runner only for where its configuration exports.
func syncFacts(configSyncDirectory string) drush.Facts {
	return drush.Facts{ConfigSyncDirectory: configSyncDirectory}
}
