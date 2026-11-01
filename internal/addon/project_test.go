package addon

import (
	"encoding/json"

	"github.com/drupdater/drupdater/internal/composer"
)

// lockedProject is a project with a web root whose lock has each of names.
func lockedProject(names ...string) composer.Project {
	project := composer.Project{WebRoot: "web", Packages: map[string]composer.LockedPackage{}}
	for _, name := range names {
		project.Packages[name] = composer.LockedPackage{Version: "1.0.0"}
	}
	return project
}

// withCustomCode is project with dirs as its custom code directories.
func withCustomCode(project composer.Project, dirs ...string) composer.Project {
	project.CustomCodeDirectories = dirs
	return project
}

// withCore is project with drupal/core locked at version.
func withCore(project composer.Project, version string) composer.Project {
	project.Packages["drupal/core"] = composer.LockedPackage{Version: version}
	return project
}

// patchedProject is a project whose composer.json declares patches, "" for none, and whose lock
// has installed and dependencies declaring depPatches.
func patchedProject(patches string, depPatches map[string][]string, installed ...string) composer.Project {
	project := lockedProject(installed...)
	project.DependencyPatches = depPatches
	if patches != "" {
		project.Patches = json.RawMessage(patches)
	}
	return project
}
