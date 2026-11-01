package siteinstall

import "github.com/drupdater/drupdater/internal/composer"

// atWebRoot is a project whose web root is webRoot.
func atWebRoot(webRoot string) composer.Project {
	return composer.Project{WebRoot: webRoot}
}
