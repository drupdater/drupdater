package services

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/afero"
)

// CheckResult is one preflight outcome, from the "drupdater check" command or from the
// fail-fast guard at the start of a real update.
type CheckResult struct {
	Name string
	OK   bool
	// Detail explains a failure; empty on a plain pass.
	Detail string
}

func checkOK(name string) CheckResult {
	return CheckResult{Name: name, OK: true}
}

func checkFailed(name string, detail string) CheckResult {
	return CheckResult{Name: name, OK: false, Detail: detail}
}

// ShallowCloneChecker is all CheckGitHistoryComplete needs, so a caller outside this package can
// supply a small double rather than implement every Repository method.
type ShallowCloneChecker interface {
	IsShallowClone(path string) (bool, error)
}

// CheckGitHistoryComplete reports whether workingDir is a shallow clone. Such a checkout commits
// the update branch fine but fails the push with a cryptic "object not found", because the remote
// needs an ancestry it does not have. Checking upfront makes that failure actionable.
func CheckGitHistoryComplete(repository ShallowCloneChecker, workingDir string) CheckResult {
	const name = "git history complete (not a shallow clone)"

	shallow, err := repository.IsShallowClone(workingDir)
	if err != nil {
		return checkFailed(name, fmt.Sprintf("could not determine clone depth: %s", err))
	}
	if shallow {
		return checkFailed(name, `shallow checkout detected: fetch full history (set "fetch-depth: 0" in GitHub Actions, or GIT_DEPTH: "0" in GitLab CI)`)
	}
	return checkOK(name)
}

// PlatformReqsChecker is all CheckPlatformRequirements needs, narrow for the same reason as
// ShallowCloneChecker.
type PlatformReqsChecker interface {
	CheckPlatformReqs(ctx context.Context, dir string) (string, error)
}

// CheckPlatformRequirements reports whether the runtime PHP version satisfies the platform
// requirements recorded in the project's composer.lock.
func CheckPlatformRequirements(ctx context.Context, composer PlatformReqsChecker, workingDir string) CheckResult {
	const name = "PHP platform requirements satisfied"

	out, err := composer.CheckPlatformReqs(ctx, workingDir)
	if err != nil {
		return checkFailed(name, out)
	}
	return checkOK(name)
}

// ComposerConfigGetter is all CheckSiteSettings needs, narrow for the same reason as
// ShallowCloneChecker.
type ComposerConfigGetter interface {
	GetConfig(ctx context.Context, dir string, key string) (string, error)
}

// CheckSiteSettings reports whether site has a settings.php under the configured web root. Its
// absence means the site was never installed here, so every downstream phase has nothing to
// build on.
func CheckSiteSettings(ctx context.Context, composer ComposerConfigGetter, fs afero.Fs, workingDir string, site string) CheckResult {
	name := fmt.Sprintf("site %q: settings.php", site)

	webroot, err := composer.GetConfig(ctx, workingDir, "extra.drupal-scaffold.locations.web-root")
	if err != nil {
		return checkFailed(name, fmt.Sprintf("could not determine web root: %s", err))
	}
	webroot = strings.TrimSuffix(webroot, "/")

	relPath := filepath.Join(webroot, "sites", site, "settings.php")
	if _, err := fs.Stat(filepath.Join(workingDir, relPath)); err != nil {
		return checkFailed(name, fmt.Sprintf("not found at %s", relPath))
	}
	return checkOK(name)
}
