package services

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/afero"
)

// CheckResult is one named pass/fail outcome of a preflight check, either run standalone by
// the "drupdater check" command or as a fail-fast guard at the start of a real update.
type CheckResult struct {
	Name string
	OK   bool
	// Detail explains a failure. It may also carry extra context on success (currently unused
	// there); empty on a plain pass.
	Detail string
}

func checkOK(name string) CheckResult {
	return CheckResult{Name: name, OK: true}
}

func checkFailed(name string, detail string) CheckResult {
	return CheckResult{Name: name, OK: false, Detail: detail}
}

// ShallowCloneChecker is the one capability CheckGitHistoryComplete needs. Narrower than the
// full Repository interface so callers outside this package (e.g. "drupdater check") can supply
// a small test double instead of implementing every Repository method.
type ShallowCloneChecker interface {
	IsShallowClone(path string) (bool, error)
}

// CheckGitHistoryComplete reports whether workingDir is a shallow clone. A shallow checkout
// (e.g. a CI default of fetch-depth: 1) can still build and commit the update branch, but
// pushing it fails downstream with a cryptic "object not found": the remote needs the ancestry
// of the pushed commits to describe them, and a shallow clone doesn't have it. Checking this
// upfront turns that late failure into an actionable one.
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

// PlatformReqsChecker is the one capability CheckPlatformRequirements needs. Narrower than the
// full Composer interface for the same reason as ShallowCloneChecker above.
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

// ComposerConfigGetter is the one capability CheckSiteSettings needs. Narrower than the full
// Composer interface for the same reason as ShallowCloneChecker above.
type ComposerConfigGetter interface {
	GetConfig(ctx context.Context, dir string, key string) (string, error)
}

// CheckSiteSettings reports whether site has a settings.php where Drupal expects to find it,
// derived from the project's configured web root. Its absence means the site was never
// installed in this checkout, so everything downstream (site install, update hooks, config
// export) has nothing to build on.
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
